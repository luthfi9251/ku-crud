package api

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"ku-crud/internal/engine"
	"ku-crud/internal/meta"
)

// seedFKLive creates customers(3 rows) + orders(2 rows referencing customers)
// in the live PG and stores defs: customers(id=1), orders(id=2, fk on customer_id).
func seedFKLive(t *testing.T, s *Server) {
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
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL, city varchar(80));
		CREATE TABLE orders(id serial PRIMARY KEY, note varchar(80), customer_id int REFERENCES customers(id));
		INSERT INTO customers(name,city) VALUES ('jo','Bandung'), ('joe','Jakarta'), ('ana',NULL);
		INSERT INTO orders(note,customer_id) VALUES ('o1',1), ('o2',2), ('o3',NULL);`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	cust := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 2}
	if err := s.store.SaveTableDef(cust, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "city", Label: "City", FieldType: "text", Editable: true,
			Visible: true, Searchable: true, Sortable: true, Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	orders := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 10}
	if err := s.store.SaveTableDef(orders, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "note", Label: "Note", FieldType: "text", Editable: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			FKTableDefID: cust.ID, FKRefColumn: "id", FKDisplayColumns: []string{"name", "city"},
			Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRowListRels(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s)

	w := do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/rows", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"rels":{"customer_id"`) {
		t.Fatalf("rels missing: %s", body)
	}
	// NOTE: Go maps marshal with sorted keys, so assert field presence rather
	// than exact key order (brief's literal substrings are unsatisfiable).
	if !strings.Contains(body, `"name":"jo"`) || !strings.Contains(body, `"city":"Bandung"`) ||
		!strings.Contains(body, `"name":"joe"`) || !strings.Contains(body, `"city":"Jakarta"`) {
		t.Fatalf("rels values wrong: %s", body)
	}
	if strings.Contains(body, `"3":`) {
		t.Fatalf("null fk must not be resolved: %s", body)
	}

	// single row get enriches too
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c)
	if !strings.Contains(w.Body.String(), `"rels":{"customer_id"`) ||
		!strings.Contains(w.Body.String(), `"name":"jo"`) {
		t.Fatalf("get rels: %s", w.Body)
	}

	// fkoptions: search over target searchable columns
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/fkoptions/customer_id?search=jakarta", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"joe"`) ||
		!strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("fkoptions = %d %s", w.Code, w.Body)
	}
	// fkoptions without search lists paginated (target pageSize 2)
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/fkoptions/customer_id", "", c)
	if !strings.Contains(w.Body.String(), `"pageSize":2`) {
		t.Fatalf("fkoptions pageSize = %s", w.Body)
	}
	// empty pages must serialize rows as [] (not null)
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/rows?search=zzzznomatch", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rows":[]`) {
		t.Fatalf("empty row list shape = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/fkoptions/customer_id?search=zzzznomatch", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rows":[]`) {
		t.Fatalf("empty fkoptions shape = %d %s", w.Code, w.Body)
	}
	// non-fk column → 404
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/fkoptions/note", "", c); w.Code != 404 {
		t.Fatalf("non-fk fkoptions = %d", w.Code)
	}
}

func TestRowListRelsRBAC(t *testing.T) {
	s := newTestServer(t)
	seedFKLive(t, s)

	// reader can read orders (def 2) but NOT customers (def 1)
	reader := loginAs(t, s, "ordreader", &meta.Role{Name: "OrdReader"},
		[]meta.TableGrant{{TableDefID: 2, CanRead: true}})

	w := do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/rows", "", reader)
	if w.Code != 200 {
		t.Fatalf("reader list = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), `"rels":{"customer_id"`) {
		t.Fatalf("rels must skip target without read grant: %s", w.Body)
	}

	// picker on the fk column needs read on the target too → 403
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/fkoptions/customer_id", "", reader); w.Code != 403 {
		t.Fatalf("reader fkoptions = %d", w.Code)
	}

	// grant read on customers as well → rels present and picker works
	rw := loginAs(t, s, "bothreader", &meta.Role{Name: "BothReader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}, {TableDefID: 2, CanRead: true}})
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/rows", "", rw)
	if !strings.Contains(w.Body.String(), `"rels":{"customer_id"`) {
		t.Fatalf("granted rels missing: %s", w.Body)
	}
	w = do(s, "GET", "/api/tables/"+tdTok(s, 2)+"/fkoptions/customer_id", "", rw)
	if w.Code != 200 {
		t.Fatalf("granted fkoptions = %d %s", w.Code, w.Body)
	}
}

func TestFKWriteAndDeleteProtection(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s)

	// create with dangling fk value → 400
	body := `{"note":"x","customer_id":999}`
	w := do(s, "POST", "/api/tables/"+tdTok(s, 2)+"/rows", body, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("dangling fk = %d %s", w.Code, w.Body)
	}

	// create with valid fk → 200
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 2)+"/rows", `{"note":"o9","customer_id":3}`, c); w.Code != 200 {
		t.Fatalf("valid fk create = %d %s", w.Code, w.Body)
	}

	// update to dangling fk → 400
	w = do(s, "PUT", "/api/tables/"+tdTok(s, 2)+"/rows/"+engine.EncodeRowKey([]string{"1"}),
		`{"customer_id":999}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("dangling fk update = %d %s", w.Code, w.Body)
	}

	// fk value type checked against baseType (number) → 400 on text value
	w = do(s, "POST", "/api/tables/"+tdTok(s, 2)+"/rows", `{"note":"x","customer_id":"abc"}`, c)
	if w.Code != 400 {
		t.Fatalf("bad fk type = %d %s", w.Code, w.Body)
	}

	// dangling fk message preserved after error-mapping fix
	w = do(s, "POST", "/api/tables/"+tdTok(s, 2)+"/rows", `{"note":"x","customer_id":999}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("post-fix dangling fk = %d %s", w.Code, w.Body)
	}

	// delete referenced customer (id 1, referenced by order 1) → 409 with detail
	w = do(s, "DELETE", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Orders") ||
		!strings.Contains(w.Body.String(), "customer_id") {
		t.Fatalf("delete blocked = %d %s", w.Code, w.Body)
	}

	// delete unreferenced customer (id 3 — order 'o9' references it after create above;
	// so delete the order first, then the customer succeeds)
	if w = do(s, "DELETE", "/api/tables/"+tdTok(s, 2)+"/rows/"+engine.EncodeRowKey([]string{"4"}), "", c); w.Code != 200 {
		t.Fatalf("delete order o9 = %d %s", w.Code, w.Body)
	}
	if w = do(s, "DELETE", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"3"}), "", c); w.Code != 200 {
		t.Fatalf("delete unreferenced customer = %d %s", w.Code, w.Body)
	}

	// null fk on create is fine
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 2)+"/rows", `{"note":"o10"}`, c); w.Code != 200 {
		t.Fatalf("null fk = %d %s", w.Code, w.Body)
	}
}

// seedMultiRowRef creates items keyed on the NON-UNIQUE column grp
// (('g1','r1'), ('g1','r2'), ('g2','r3')) and a metadata-only child
// item_refs(note_ref → items.note) whose single row references r2.
// Returns the def ids (items, item_refs).
func seedMultiRowRef(t *testing.T, s *Server) (int64, int64) {
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
	if _, err := db.Exec(`DROP TABLE IF EXISTS item_refs; DROP TABLE IF EXISTS items;
		CREATE TABLE items(grp text NOT NULL, note text NOT NULL);
		CREATE TABLE item_refs(id serial PRIMARY KEY, note_ref text);
		INSERT INTO items(grp,note) VALUES ('g1','r1'), ('g1','r2'), ('g2','r3');
		INSERT INTO item_refs(note_ref) VALUES ('r2');`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	items := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "items",
		Label: "Items", KeyColumns: []string{"grp"}, PageSize: 10}
	if err := s.store.SaveTableDef(items, []meta.ColumnDef{
		{Name: "grp", Label: "Group", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "note", Label: "Note", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	refs := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "item_refs",
		Label: "ItemRefs", KeyColumns: []string{"id"}, PageSize: 10}
	if err := s.store.SaveTableDef(refs, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "note_ref", Label: "Item", FieldType: "fk", BaseType: "text",
			FKTableDefID: items.ID, FKRefColumn: "note", FKDisplayColumns: []string{"grp"},
			Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	return items.ID, refs.ID
}

func TestDeleteProtectionMultiRow(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	itemsID, refsID := seedMultiRowRef(t, s)

	// delete via the NON-UNIQUE key grp='g1' matches r1 AND r2; r2 (a later
	// match, metadata-only relation) is referenced by item_refs → 409
	w := do(s, "DELETE", "/api/tables/"+tdTok(s, itemsID)+"/rows/"+engine.EncodeRowKey([]string{"g1"}), "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "ItemRefs") ||
		!strings.Contains(w.Body.String(), "note_ref") {
		t.Fatalf("multi-row delete blocked = %d %s", w.Code, w.Body)
	}

	// remove the referencing child row, retry → both g1 rows deleted
	if w = do(s, "DELETE", "/api/tables/"+tdTok(s, refsID)+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c); w.Code != 200 {
		t.Fatalf("delete child = %d %s", w.Code, w.Body)
	}
	w = do(s, "DELETE", "/api/tables/"+tdTok(s, itemsID)+"/rows/"+engine.EncodeRowKey([]string{"g1"}), "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"affected":2`) {
		t.Fatalf("retry delete = %d %s", w.Code, w.Body)
	}
	// only the g2 row remains
	w = do(s, "GET", "/api/tables/"+tdTok(s, itemsID)+"/rows", "", c)
	if !strings.Contains(w.Body.String(), `"total":1`) || strings.Contains(w.Body.String(), "r1") {
		t.Fatalf("items after delete: %s", w.Body)
	}
}

// seedFKByName creates customers keyed on id and an orders def whose fk
// column references customers.name (NOT the key column), display ["city"].
func seedFKByName(t *testing.T, s *Server) (int64, int64) {
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
	if _, err := db.Exec(`DROP TABLE IF EXISTS orders; DROP TABLE IF EXISTS customers;
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL, city varchar(80));
		CREATE TABLE orders(id serial PRIMARY KEY, note varchar(80), customer_name varchar(80));
		INSERT INTO customers(name,city) VALUES ('jo','Bandung'), ('joe','Jakarta');
		INSERT INTO orders(note,customer_name) VALUES ('o1','jo');`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	cust := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.store.SaveTableDef(cust, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "city", Label: "City", FieldType: "text", Editable: true,
			Visible: true, Searchable: true, Sortable: true, Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	orders := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 10}
	if err := s.store.SaveTableDef(orders, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "note", Label: "Note", FieldType: "text", Editable: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "customer_name", Label: "Customer", FieldType: "fk", BaseType: "text",
			FKTableDefID: cust.ID, FKRefColumn: "name", FKDisplayColumns: []string{"city"},
			Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	return cust.ID, orders.ID
}

func TestFKOptionsNonKeyRefColumn(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	_, ordersID := seedFKByName(t, s)

	// fk column refs customers.name while the target is keyed on id — the
	// sort column must still be selectable (previously a 400)
	w := do(s, "GET", "/api/tables/"+tdTok(s, ordersID)+"/fkoptions/customer_name", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("fkoptions non-key ref column = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"name":"jo"`) || !strings.Contains(w.Body.String(), `"city":"Bandung"`) {
		t.Fatalf("fkoptions rows wrong: %s", w.Body)
	}
}
