package api

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-crud/core/engine"
	"ku-crud/internal/meta"
)

// seedComposite resets the shared live schema and builds:
//   - def 1: customers (single key "id")
//   - def 2: order_items with composite key (order_id, item_id)
func seedComposite(t *testing.T, s *Server) {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	db, err := sql.Open("pgx", cs)
	if err != nil {
		t.Skipf("no PG: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("no PG: %v", err)
	}
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;
		CREATE TABLE order_items(order_id int NOT NULL, item_id int NOT NULL,
			qty int NOT NULL DEFAULT 1, note text,
			PRIMARY KEY(order_id, item_id));
		INSERT INTO order_items(order_id,item_id,qty,note) VALUES
			(1, 5, 2, 'first'), (1, 9, 1, 'second'), (2, 5, 7, NULL);`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "order_items",
		Label: "Order Items", KeyColumns: []string{"order_id", "item_id"}, PageSize: 10}
	cols := []meta.ColumnDef{
		{Name: "order_id", Label: "Order", FieldType: "number", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "item_id", Label: "Item", FieldType: "number", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "qty", Label: "Qty", FieldType: "number", Editable: true,
			Visible: true, Sortable: true, Position: 2},
		{Name: "note", Label: "Note", FieldType: "text", Editable: true,
			Visible: true, Searchable: true, Position: 3},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func key2(a, b string) string { return engine.EncodeRowKey([]string{a, b}) }

func TestCompositeKeyCRUD(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedComposite(t, s)
	tok := defName(s, 1)

	// list: 3 rows
	w := do(s, "GET", "/api/data/"+tok+"/rows", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":3`) {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}

	// get by composite key
	w = do(s, "GET", "/api/data/"+tok+"/rows/"+key2("1", "9"), "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"note":"second"`) {
		t.Fatalf("get = %d %s", w.Code, w.Body)
	}

	// wrong arity → 400
	if w = do(s, "GET", "/api/data/"+tok+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c); w.Code != 400 {
		t.Fatalf("single-value key = %d", w.Code)
	}
	// garbage key → 400
	if w = do(s, "GET", "/api/data/"+tok+"/rows/garbage", "", c); w.Code != 400 {
		t.Fatalf("garbage key = %d", w.Code)
	}

	// update by composite key (touch only qty)
	w = do(s, "PUT", "/api/data/"+tok+"/rows/"+key2("1", "9"), `{"qty":4}`, c)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/data/"+tok+"/rows/"+key2("1", "9"), "", c)
	if !strings.Contains(w.Body.String(), `"qty":4`) {
		t.Fatalf("after update = %s", w.Body)
	}

	// insert new composite row
	if w = do(s, "POST", "/api/data/"+tok+"/rows",
		`{"order_id":3,"item_id":2,"qty":9,"note":"new"}`, c); w.Code != 200 {
		t.Fatalf("insert = %d %s", w.Code, w.Body)
	}

	// delete by composite key
	if w = do(s, "DELETE", "/api/data/"+tok+"/rows/"+key2("2", "5"), "", c); w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/data/"+tok+"/rows/"+key2("2", "5"), "", c); w.Code != 404 {
		t.Fatalf("deleted = %d", w.Code)
	}

	// audit row_pk is the JSON key array
	w = do(s, "GET", "/api/audit?tableDefId="+tdTok(s, 1), "", c)
	if w.Code != 200 {
		t.Fatalf("audit = %d %s", w.Code, w.Body)
	}
	{
		var res struct {
			Entries []struct {
				Action string `json:"action"`
				RowPK  string `json:"rowPk"`
			} `json:"entries"`
		}
		json.Unmarshal(w.Body.Bytes(), &res)
		var sawJSONKey bool
		for _, e := range res.Entries {
			var arr []string
			if e.Action == "DELETE" && json.Unmarshal([]byte(e.RowPK), &arr) == nil &&
				len(arr) == 2 && arr[0] == "2" && arr[1] == "5" {
				sawJSONKey = true
			}
		}
		if !sawJSONKey {
			t.Fatalf("audit row_pk not composite JSON: %s", w.Body)
		}
	}
}

func TestCompositeGrantsEndToEnd(t *testing.T) {
	s := newTestServer(t)
	seedComposite(t, s)
	tok := defName(s, 1)

	reader := loginAs(t, s, "rou", &meta.Role{Name: "RO"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	writer := loginAs(t, s, "rwx", &meta.Role{Name: "RWX"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true, CanCreate: true, CanUpdate: true, CanDelete: true}})

	// reader: list ok, write forbidden
	if w := do(s, "GET", "/api/data/"+tok+"/rows", "", reader); w.Code != 200 {
		t.Fatalf("reader list = %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/data/"+tok+"/rows", `{"order_id":9,"item_id":9}`, reader); w.Code != 403 {
		t.Fatalf("reader insert = %d", w.Code)
	}
	// writer: full CRUD through the same encoded key path
	if w := do(s, "POST", "/api/data/"+tok+"/rows", `{"order_id":9,"item_id":9,"qty":1}`, writer); w.Code != 200 {
		t.Fatalf("writer insert = %d %s", w.Code, w.Body)
	}
	if w := do(s, "PUT", "/api/data/"+tok+"/rows/"+key2("9", "9"), `{"qty":5}`, writer); w.Code != 200 {
		t.Fatalf("writer update = %d %s", w.Code, w.Body)
	}
	if w := do(s, "DELETE", "/api/data/"+tok+"/rows/"+key2("9", "9"), "", writer); w.Code != 200 {
		t.Fatalf("writer delete = %d %s", w.Code, w.Body)
	}
}

func TestRowEndpointInjectionAttempts(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedComposite(t, s)
	tok := defName(s, 1)

	// sort/dir injection attempts: bad values never reach SQL as-is
	for _, q := range []string{
		"sort=id;DROP&dir=ASC",
		"sort=order_id&dir=ASC;DELETE%20FROM%20order_items",
		"sort=(select)&dir=ASC",
	} {
		if w := do(s, "GET", "/api/data/"+tok+"/rows?"+q, "", c); w.Code != 200 {
			t.Fatalf("list %q = %d %s", q, w.Code, w.Body)
		}
	}
	// search with wildcards/quotes must be inert (escaped) and not blow up
	if w := do(s, "GET", "/api/data/"+tok+"/rows?search=first%25--", "", c); w.Code != 200 {
		t.Fatalf("wildcard search = %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/api/data/"+tok+"/rows?search=%27%20OR%201%3D1--", "", c); w.Code != 200 {
		t.Fatalf("quote search = %d %s", w.Code, w.Body)
	}
	// key columns payload injection on def create rejected
	bad := `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"order_items",
"label":"X","keyColumns":["order_id; DROP TABLE order_items"],"pageSize":20,"columns":[
 {"name":"order_id","label":"o","fieldType":"number","editable":true,"required":true,"visible":true,"position":0}]}`
	if w := do(s, "POST", "/api/tables", bad, c); w.Code != 400 {
		t.Fatalf("key injection def = %d %s", w.Code, w.Body)
	}
	// page param garbage is inert
	if w := do(s, "GET", "/api/data/"+tok+"/rows?page=-5", "", c); w.Code != 200 {
		t.Fatalf("negative page = %d", w.Code)
	}
}
