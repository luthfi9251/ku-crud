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

// seedLive spins up a live-table fixture: 3 customers rows + a full table def.
func seedLive(t *testing.T, s *Server) {
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
		CREATE TYPE weather AS ENUM ('sunny','rainy');
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL,
			active bool DEFAULT true, balance numeric(10,2), born date, status weather);
		INSERT INTO customers(name,balance,status) VALUES
			('jo', 10.5, 'sunny'), ('joe', 2, 'rainy'), ('ana', 0, 'sunny')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err) // id 1 — keeps ids deterministic
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 2, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 2}
	cols := []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "active", Label: "Active", FieldType: "boolean", Editable: true,
			Visible: true, Position: 2},
		{Name: "balance", Label: "Balance", FieldType: "number", Editable: true,
			Visible: true, Sortable: true, Position: 3},
		{Name: "born", Label: "Born", FieldType: "datetime", Editable: true,
			Visible: true, Position: 4},
		{Name: "status", Label: "Status", FieldType: "enum", EnumOptions: []string{"sunny", "rainy"},
			Editable: true, Visible: true, Searchable: true, Position: 5},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func TestRowListAndGet(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)

	w := do(s, "GET", "/api/data/"+defName(s, 1)+"/rows", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"total":3`) ||
		!strings.Contains(w.Body.String(), `"pageSize":2`) {
		t.Fatalf("page1 = %s", w.Body)
	}

	w = do(s, "GET", "/api/data/"+defName(s, 1)+"/rows?search=jo", "", c)
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("search = %s", w.Body)
	}

	w = do(s, "GET", "/api/data/"+defName(s, 1)+"/rows?sort=name&dir=DESC", "", c)
	body := w.Body.String()
	// DESC: "joe" sorts before "jo" (prefix compares less); brief's original
	// `<` was inverted — it fired on correct DESC output.
	if strings.Index(body, `"joe"`) > strings.Index(body, `"jo"`) {
		t.Fatalf("sort desc wrong: %s", body)
	}

	// sort on non-sortable column (born) silently falls back to pk ASC
	w = do(s, "GET", "/api/data/"+defName(s, 1)+"/rows?sort=born&dir=DESC", "", c)
	if !strings.Contains(w.Body.String(), `"total":3`) {
		t.Fatalf("fallback sort = %s", w.Body)
	}

	// page 2 with pageSize 2 → last row only
	w = do(s, "GET", "/api/data/"+defName(s, 1)+"/rows?page=2", "", c)
	if !strings.Contains(w.Body.String(), `"total":3`) ||
		strings.Count(w.Body.String(), `"name":`) != 1 {
		t.Fatalf("page2 = %s", w.Body)
	}

	w = do(s, "GET", "/api/data/"+defName(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"2"}), "", c)
	if !strings.Contains(w.Body.String(), `"name":"joe"`) {
		t.Fatalf("get row = %s", w.Body)
	}
	if w = do(s, "GET", "/api/data/"+defName(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"999"}), "", c); w.Code != 404 {
		t.Fatalf("missing row = %d", w.Code)
	}
}

// seedSortable creates a 4-row table with a name column to test default sort.
func seedSortable(t *testing.T, s *Server, defaultSortCol, defaultSortDir string) string {
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
		CREATE TABLE items(id serial PRIMARY KEY, name text NOT NULL, note text);
		INSERT INTO items(name) VALUES ('delta'), ('alpha'), ('gamma'), ('beta')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "items",
		Label: "Items", KeyColumns: []string{"id"}, PageSize: 10,
		DefaultSortCol: defaultSortCol, DefaultSortDir: defaultSortDir}
	cols := []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "note", Label: "Note", FieldType: "text", Editable: true,
			Visible: true, Sortable: false, Position: 2},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	return def.TableName
}

func firstNames(t *testing.T, body string) []string {
	t.Helper()
	var res struct {
		Rows []struct {
			Name string `json:"name"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	out := make([]string, len(res.Rows))
	for i, r := range res.Rows {
		out[i] = r.Name
	}
	return out
}

func TestDefaultSort(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedSortable(t, s, "name", "DESC")

	w := do(s, "GET", "/api/data/"+tok+"/rows", "", c)
	got := firstNames(t, w.Body.String())
	want := []string{"gamma", "delta", "beta", "alpha"}
	if !equalSlice(got, want) {
		t.Fatalf("default sort DESC: %v", got)
	}

	// explicit sort overrides the default
	w = do(s, "GET", "/api/data/"+tok+"/rows?sort=name&dir=ASC", "", c)
	got = firstNames(t, w.Body.String())
	if !equalSlice(got, []string{"alpha", "beta", "delta", "gamma"}) {
		t.Fatalf("explicit override: %v", got)
	}

	// non-sortable explicit column falls back to the default sort
	w = do(s, "GET", "/api/data/"+tok+"/rows?sort=note&dir=DESC", "", c)
	got = firstNames(t, w.Body.String())
	if !equalSlice(got, want) {
		t.Fatalf("invalid explicit falls to default: %v", got)
	}
}

func TestDefaultSortInvalidColumnFallsBackToKey(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	// default sort column dropped from live → resolveSort falls back to key ASC
	tok := seedSortable(t, s, "gone", "DESC")
	w := do(s, "GET", "/api/data/"+tok+"/rows", "", c)
	got := firstNames(t, w.Body.String())
	// insertion order by id: delta, alpha, gamma, beta
	if !equalSlice(got, []string{"delta", "alpha", "gamma", "beta"}) {
		t.Fatalf("key fallback: %v", got)
	}
}

// TestDataNamespace pins the /api/data namespace: the def fetch is
// name-addressed and byte-compatible with the management def detail,
// nameless query views are addressed by their masked id token, and the
// old id-addressed data routes are gone.
func TestDataNamespace(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	if w := do(s, "POST", "/api/tables", defBody(s), c); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}

	// def by name ≡ def by id (same serialization)
	byID := do(s, "GET", "/api/tables/"+tdTok(s, 1), "", c)
	byName := do(s, "GET", "/api/data/customers", "", c)
	if byID.Code != 200 || byName.Code != 200 || byID.Body.String() != byName.Body.String() {
		t.Fatalf("def by name = %d %s (by id = %d)", byName.Code, byName.Body, byID.Code)
	}

	// unknown name → the management def-detail 404 shape
	if w := do(s, "GET", "/api/data/nope", "", c); w.Code != 404 ||
		!strings.Contains(w.Body.String(), "table def not found") {
		t.Fatalf("unknown name = %d %s", w.Code, w.Body)
	}

	// nameless query view: token-addressed, same def JSON both ways
	seedQueryDef(t, s, []string{"n"})
	qByToken := do(s, "GET", "/api/data/"+tdTok(s, 2), "", c)
	qByID := do(s, "GET", "/api/tables/"+tdTok(s, 2), "", c)
	if qByToken.Code != 200 || qByToken.Body.String() != qByID.Body.String() {
		t.Fatalf("query def by token = %d %s", qByToken.Code, qByToken.Body)
	}

	// a token pointing at a NAMED def never resolves (names own the
	// namespace); the old id-addressed data routes are gone
	if w := do(s, "GET", "/api/data/"+tdTok(s, 1), "", c); w.Code != 404 {
		t.Fatalf("named def by token = %d", w.Code)
	}
	if w := do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows", "", c); w.Code != 404 {
		t.Fatalf("old row route = %d", w.Code)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
