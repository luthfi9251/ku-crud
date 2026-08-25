package api

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/luthfi9251/kucrud-core/engine"
	"ku-crud/internal/meta"
)

func TestRowWriteAndAudit(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)

	// CREATE
	w := do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows",
		`{"name":"nia","active":false,"balance":7.25,"born":"1990-01-02","status":"rainy"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}

	// CREATE with unknown column → VALIDATION
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"name":"x","hax":1}`, c); w.Code != 400 {
		t.Fatalf("unknown col = %d %s", w.Code, w.Body)
	}
	// CREATE missing required (name) → VALIDATION
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"active":true}`, c); w.Code != 400 {
		t.Fatalf("missing required = %d %s", w.Code, w.Body)
	}
	// bad enum → VALIDATION
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"name":"y","status":"snowy"}`, c); w.Code != 400 {
		t.Fatalf("bad enum = %d %s", w.Code, w.Body)
	}
	// bad datetime → VALIDATION
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"name":"y","born":"02/01/1990"}`, c); w.Code != 400 {
		t.Fatalf("bad datetime = %d %s", w.Code, w.Body)
	}
	// key column (id) is a known field during insert (PKs are insertable),
	// but the required name is still missing → VALIDATION
	if w = do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":9}`, c); w.Code != 400 {
		t.Fatalf("missing required with key = %d %s", w.Code, w.Body)
	}
	// non-editable column (id) rejected on UPDATE
	if w = do(s, "PUT", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"1"}), `{"id":99}`, c); w.Code != 400 {
		t.Fatalf("non-editable update = %d %s", w.Code, w.Body)
	}

	// UPDATE row 1
	w = do(s, "PUT", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"1"}), `{"name":"jo!"}`, c)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c); !strings.Contains(w.Body.String(), `"name":"jo!"`) {
		t.Fatalf("row after update = %s", w.Body)
	}

	// DELETE row 4 (nia)
	if w = do(s, "DELETE", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"4"}), "", c); w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"4"}), "", c); w.Code != 404 {
		t.Fatalf("deleted row still there = %d", w.Code)
	}

	// audit returns in Task 11 (platformhooks): assertions for
	// 1 INSERT + 1 UPDATE + 1 DELETE audit entries (total=3, INSERT new
	// values, UPDATE old values containing "jo", DELETE old values
	// containing "nia") removed with the write path's audit decoupling.
}

// seedUUIDJSON creates an assets table (uuid PK + jsonb + json columns) and
// a matching definition (def id 2) on the live PG fixture.
func seedUUIDJSON(t *testing.T, s *Server) {
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
		CREATE TABLE assets(id uuid PRIMARY KEY, meta jsonb NOT NULL, doc json);
		INSERT INTO assets(id, meta) VALUES
			('123e4567-e89b-12d3-a456-426614174000', '{"k":"v"}'),
			('00000000-0000-0000-0000-000000000001', '{"k":"w"}')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "live2", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: cs}); err != nil {
		t.Fatal(err)
	}
	def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "assets",
		Label: "Assets", KeyColumns: []string{"id"}, PageSize: 20}
	cols := []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "uuid", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "meta", Label: "Meta", FieldType: "json", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "doc", Label: "Doc", FieldType: "json", Editable: true,
			Visible: true, Position: 2},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func TestUUIDJSONColumns(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedUUIDJSON(t, s)
	tok := tdTok(s, 1)

	// list: both rows, uuid key round-trips
	w := do(s, "GET", "/api/tables/"+tok+"/rows", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}

	// invalid uuid → 400
	if w = do(s, "POST", "/api/tables/"+tok+"/rows",
		`{"id":"not-a-uuid","meta":{"a":1}}`, c); w.Code != 400 {
		t.Fatalf("bad uuid = %d %s", w.Code, w.Body)
	}
	// invalid json string → 400
	if w = do(s, "POST", "/api/tables/"+tok+"/rows",
		`{"id":"123e4567-e89b-12d3-a456-426614174009","meta":"{a}"}`, c); w.Code != 400 {
		t.Fatalf("bad json = %d %s", w.Code, w.Body)
	}
	// valid create with object + string forms
	w = do(s, "POST", "/api/tables/"+tok+"/rows",
		`{"id":"123e4567-e89b-12d3-a456-426614174009","meta":{"n":1},"doc":"[1,2]"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	key := engine.EncodeRowKey([]string{"123e4567-e89b-12d3-a456-426614174009"})
	w = do(s, "GET", "/api/tables/"+tok+"/rows/"+key, "", c)
	if w.Code != 200 {
		t.Fatalf("get after create = %d %s", w.Code, w.Body)
	}
	var getRes struct {
		Row struct {
			Meta string `json:"meta"`
			Doc  string `json:"doc"`
		} `json:"row"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &getRes); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// PG jsonb normalizes spacing on output; parse-compare instead of string-compare
	if metaTrim(getRes.Row.Meta) != `{"n":1}` {
		t.Fatalf("meta roundtrip = %q", getRes.Row.Meta)
	}
	if getRes.Row.Doc != `[1,2]` {
		t.Fatalf("doc roundtrip = %q", getRes.Row.Doc)
	}
	// update json column with invalid text → 400
	if w = do(s, "PUT", "/api/tables/"+tok+"/rows/"+key, `{"doc":"nope{"}`, c); w.Code != 400 {
		t.Fatalf("bad json update = %d %s", w.Code, w.Body)
	}
	// search inside json column works via ::text cast (jsonb output has
	// cosmetic spaces, so search a plain fragment)
	if w = do(s, "GET", "/api/tables/"+tok+"/rows?search="+encodeQuery(`w`), "", c); !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("json search = %s", w.Body)
	}
}

func encodeQuery(s string) string {
	r := strings.NewReplacer("%", "%25", "&", "%26", "+", "%2B", " ", "%20", `"`, "%22")
	return r.Replace(s)
}

// metaTrim removes jsonb's cosmetic spaces so values compare stably.
func metaTrim(s string) string {
	var b strings.Builder
	inStr := false
	for _, r := range s {
		switch {
		case r == '"':
			inStr = !inStr
			b.WriteRune(r)
		case !inStr && (r == ' ' || r == '\n' || r == '\t'):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestValidationRuleBlocksWriteAndImport(t *testing.T) {
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)

	// PUT def 1 back with an email rule on the name column
	tok := tdTok(s, 1)
	w := do(s, "GET", "/api/tables/"+tok, "", c)
	var def map[string]any
	json.Unmarshal(w.Body.Bytes(), &def)
	for _, colAny := range def["columns"].([]any) {
		col := colAny.(map[string]any)
		if col["name"] == "name" {
			col["validations"] = []map[string]any{{"type": "email"}}
		}
	}
	w = do(s, "PUT", "/api/tables/"+tok, string(mustJSON(def)), c)
	if w.Code != 200 {
		t.Fatalf("def update = %d %s", w.Code, w.Body)
	}

	// row create with a bad email -> 400
	w = do(s, "POST", "/api/tables/"+tok+"/rows", `{"id":99,"name":"not-an-email"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "email") {
		t.Fatalf("rule not enforced on create: %d %s", w.Code, w.Body)
	}

	// import preview marks the row invalid
	req := importRequest(t, "/api/tables/"+tok+"/import/preview", "id,name\n9,also-bad\n", nil)
	req.Header.Set("Cookie", *c)
	resp := httptest.NewRecorder()
	s.Routes().ServeHTTP(resp, req)
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "email") {
		t.Fatalf("rule not enforced on import: %d %s", resp.Code, resp.Body)
	}
}
