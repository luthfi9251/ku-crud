package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

// rowKeyToken encodes composite row key values the way the API expects them
// in the URL (base64url JSON array).
func rowKeyToken(vals []string) string {
	b, _ := json.Marshal(vals)
	return base64.RawURLEncoding.EncodeToString(b)
}

// seedQueryDef stores a query def directly (no live DB needed for guards).
func seedQueryDef(t *testing.T, s *Server, keys []string) {
	t.Helper()
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 1, SourceType: "query",
		QuerySQL: "SELECT name AS n FROM customers", Label: "Q",
		KeyColumns: keys, PageSize: 20}
	cols := []meta.ColumnDef{{Name: "n", Label: "N", FieldType: "text",
		Visible: true, Searchable: true, Sortable: true, Position: 0}}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func TestQueryDefWriteGuards(t *testing.T) {
	s := newTestServer(t)
	c := login(s) // alice = first user = Admin
	seedQueryDef(t, s, []string{"n"})
	tok := tdTok(s, 1)
	pk := rowKeyToken([]string{"jo"}) // helper from rows_composite_test.go
	endpoints := []struct{ method, path string }{
		{"POST", "/api/tables/" + tok + "/rows"},
		{"PUT", "/api/tables/" + tok + "/rows/" + pk},
		{"DELETE", "/api/tables/" + tok + "/rows/" + pk},
		{"POST", "/api/tables/" + tok + "/rows/bulk-delete"},
		{"GET", "/api/tables/" + tok + "/fkoptions/n"},
		{"GET", "/api/tables/" + tok + "/m2moptions/n"},
		{"GET", "/api/tables/" + tok + "/rows/" + pk + "/m2m/n"},
	}
	for _, e := range endpoints {
		w := do(s, e.method, e.path, "{}", c)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "QUERY_READONLY") {
			t.Fatalf("%s %s = %d %s", e.method, e.path, w.Code, w.Body)
		}
	}
	// admin included — query views have no write path at all
	w := do(s, "POST", "/api/tables/"+tok+"/rows", `{"n":"x"}`, c)
	if w.Code != 403 {
		t.Fatalf("admin write = %d %s", w.Code, w.Body)
	}
}

func TestQueryDefNoKeyRowGet(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedQueryDef(t, s, nil)
	w := do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows/anything", "", c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "QUERY_NO_KEY") {
		t.Fatalf("row get no-key = %d %s", w.Code, w.Body)
	}
}

func TestQueryDefValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	dsTok := s.ids.Encode("ds", 1)
	body := func(extra string) string {
		return `{"datasourceId":"` + dsTok + `","label":"Q","sourceType":"query",` + extra +
			`,"pageSize":20,"keyColumns":[],"columns":[` +
			`{"name":"n","label":"N","fieldType":"text","visible":true,"searchable":true,"sortable":true,"position":0}]}`
	}
	// dead datasource → EXPLAIN fails → 400 QUERY_INVALID
	w := do(s, "POST", "/api/tables", body(`"querySql":"SELECT 1 AS n"`), c)
	if w.Code != 400 {
		t.Fatalf("explain-on-save = %d %s", w.Code, w.Body)
	}
	// prefix check fires before any connection is attempted
	w = do(s, "POST", "/api/tables", body(`"querySql":"DELETE FROM x"`), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "SELECT or WITH") {
		t.Fatalf("prefix check = %d %s", w.Code, w.Body)
	}
	// fk columns rejected on query defs
	w = do(s, "POST", "/api/tables", `{"datasourceId":"`+dsTok+`","label":"Q","sourceType":"query",`+
		`"querySql":"SELECT 1 AS n","pageSize":20,"keyColumns":[],"columns":[`+
		`{"name":"n","label":"N","fieldType":"fk","baseType":"number","fkTableDefId":"self",`+
		`"fkRefColumn":"n","fkDisplayColumns":["n"],"position":0}]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "query view") {
		t.Fatalf("fk on query = %d %s", w.Code, w.Body)
	}
}
