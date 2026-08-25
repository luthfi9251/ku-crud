package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	kucrud "github.com/luthfi9251/kucrud-core"
	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/engine"
	"github.com/luthfi9251/kucrud-core/hooks"
)

// ---- live harness (self-skipping without KUCRUD_TEST_* env) ----

type liveDSN struct {
	driver string
	raw    string
}

func livePG(t *testing.T) liveDSN {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	return liveDSN{driver: "postgres", raw: cs}
}

func liveMySQL(t *testing.T) liveDSN {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_MYSQL")
	if cs == "" {
		t.Skip("KUCRUD_TEST_MYSQL not set")
	}
	return liveDSN{driver: "mysql", raw: cs}
}

func openSQL(t *testing.T, d liveDSN) *sql.DB {
	t.Helper()
	sqlDriver := d.driver
	if sqlDriver == "postgres" {
		sqlDriver = "pgx" // registered by ds's pgx stdlib blank import
	}
	db, err := sql.Open(sqlDriver, d.raw)
	if err != nil {
		t.Skipf("no %s: %v", d.driver, err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Skipf("no %s: %v", d.driver, err)
	}
	return db
}

const t9Table = "t9_widgets"

func createT9(t *testing.T, db *sql.DB, driver string) {
	t.Helper()
	var ddl []string
	if driver == "postgres" {
		ddl = []string{fmt.Sprintf(`DROP TABLE IF EXISTS %s`, t9Table),
			fmt.Sprintf(`CREATE TABLE %s(
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			score NUMERIC(12,2),
			created_at TIMESTAMPTZ DEFAULT now())`, t9Table)}
	} else {
		ddl = []string{fmt.Sprintf(`DROP TABLE IF EXISTS %s`, t9Table),
			fmt.Sprintf(`CREATE TABLE %s(
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			email VARCHAR(255),
			score DECIMAL(12,2),
			created_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP)`, t9Table)}
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	t.Cleanup(func() { db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", t9Table)) })
}

func t9Def() kucrud.Def {
	return kucrud.Def{
		Table: t9Table,
		Columns: []kucrud.Override{
			{Name: "name", Label: "Widget", Required: true},
			{Name: "email", Validation: []defs.ValidationRule{{Type: "email"}}},
			{Name: "score", Format: `{"number":{"decimals":2}}`},
		},
		DefaultSort: kucrud.Sort("id", kucrud.Asc),
		Hooks: map[kucrud.Event][]string{
			kucrud.BeforeCreate: {"t9stamp"},
		},
	}
}

// runLiveSuite drives the brief's Step 1 E2E: one registered PG/MySQL table
// with overrides, mounted as a bare Resource on a HOST mux at a custom path
// with host middleware in front, plus the App mux (CRUD sugar + /api/defs).
func runLiveSuite(t *testing.T, d liveDSN) {
	db := openSQL(t, d)
	createT9(t, db, d.driver)

	reg := hooks.NewRegistry()
	if err := reg.Register("t9stamp", func(ctx context.Context, hc *hooks.HookContext,
		ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		if ev == hooks.BeforeCreate {
			if s, ok := row.Values["name"].(string); ok {
				row.Values["name"] = "hooked:" + s
			}
		}
		return row, nil
	}); err != nil {
		t.Fatal(err)
	}
	gate := func(r *http.Request, op kucrud.Op, table string) error {
		if op == kucrud.OpDelete && r.Header.Get("X-Block-Delete") == "1" {
			return errors.New("delete blocked by host gate")
		}
		return nil
	}

	app, err := kucrud.New(kucrud.Conn{Driver: d.driver, Raw: d.raw},
		kucrud.WithHookRegistry(reg), kucrud.WithGate(gate))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { app.Close() })

	// PRIMARY API: bare handler the host mounts wherever it wants.
	res, err := app.Resource("widgets", t9Def())
	if err != nil {
		t.Fatalf("Resource: %v", err)
	}
	// SUGAR API: same def on the App's own mux (re-register by name).
	app.CRUD("/api/data/widgets", t9Def())

	// HOST mux: middleware in front + custom mount path, no StripPrefix.
	var hits int
	host := http.NewServeMux()
	host.Handle("/api/v1/widgets/", res)
	host.Handle("/api/", app)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("X-Host-Middleware", "yes")
		host.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	do := func(method, path, body string, hdr map[string]string) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, rd)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	codeOK := func(code int, want int, body string) {
		t.Helper()
		if code != want {
			t.Fatalf("status %d, want %d (body %s)", code, want, body)
		}
	}

	// Insert with a before-hook that mutates the row.
	code, body := do("POST", "/api/v1/widgets/rows",
		`{"name":"alpha","email":"a@x.io","score":1.5}`, nil)
	codeOK(code, 200, body)
	var ins struct {
		Ok bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(body), &ins); err != nil || !ins.Ok {
		t.Fatalf("insert resp %q err %v", body, err)
	}

	// 400 on a validation-rule failure.
	code, body = do("POST", "/api/v1/widgets/rows",
		`{"name":"bad","email":"notanemail"}`, nil)
	codeOK(code, 400, body)
	if !strings.Contains(body, `"code":"VALIDATION"`) || !strings.Contains(body, "email") {
		t.Fatalf("validation 400 body: %s", body)
	}

	// 400 on a required-column miss.
	code, body = do("POST", "/api/v1/widgets/rows", `{"email":"b@x.io"}`, nil)
	codeOK(code, 400, body)

	// List + sort + search + filter; proves the hook mutation landed.
	key := engine.EncodeRowKey([]string{"1"})
	list := func(q string) (int, map[string]any) {
		code, body := do("GET", "/api/v1/widgets/rows"+q, "", nil)
		var m map[string]any
		json.Unmarshal([]byte(body), &m)
		return code, m
	}
	code, m := list("")
	codeOK(code, 200, "")
	if m["total"].(float64) < 1 || len(m["rows"].([]any)) < 1 {
		t.Fatalf("list: %v", m)
	}
	if got := m["rows"].([]any)[0].(map[string]any)["name"]; got != "hooked:alpha" {
		t.Fatalf("hook mutation not persisted: %v", got)
	}
	if _, ok := m["rels"]; !ok {
		t.Fatalf("list response missing rels: %v", m)
	}
	if ps, _ := m["pageSize"].(float64); ps != 20 {
		t.Fatalf("default pageSize = %v, want 20", m["pageSize"])
	}
	code, m = list("?sort=id&dir=desc")
	codeOK(code, 200, "")
	if got := m["rows"].([]any)[0].(map[string]any)["name"]; got != "hooked:alpha" {
		t.Fatalf("sort desc: %v", m["rows"])
	}
	code, m = list("?search=hooked")
	codeOK(code, 200, "")
	if m["total"].(float64) != 1 {
		t.Fatalf("search: %v", m["total"])
	}
	code, m = list(`?filters=[{"column":"name","op":"contains","values":["hooked"]}]`)
	codeOK(code, 200, "")
	if m["total"].(float64) != 1 {
		t.Fatalf("filter: %v", m["total"])
	}

	// Get one row.
	code, body = do("GET", "/api/v1/widgets/rows/"+key, "", nil)
	codeOK(code, 200, body)
	json.Unmarshal([]byte(body), &m)
	if m["row"].(map[string]any)["name"] != "hooked:alpha" {
		t.Fatalf("get: %v", m["row"])
	}

	// Update.
	code, body = do("PUT", "/api/v1/widgets/rows/"+key, `{"name":"beta"}`, nil)
	codeOK(code, 200, body)
	code, body = do("GET", "/api/v1/widgets/rows/"+key, "", nil)
	codeOK(code, 200, body)
	json.Unmarshal([]byte(body), &m)
	if m["row"].(map[string]any)["name"] != "beta" {
		t.Fatalf("update not applied: %v", m["row"])
	}

	// 403 through the Gate.
	code, body = do("DELETE", "/api/v1/widgets/rows/"+key, "", map[string]string{"X-Block-Delete": "1"})
	codeOK(code, 403, body)
	if !strings.Contains(body, `"code":"FORBIDDEN"`) || !strings.Contains(body, "delete blocked by host gate") {
		t.Fatalf("gate 403 body: %s", body)
	}

	// CSV export.
	code, body = do("GET", "/api/v1/widgets/rows/export", "", nil)
	codeOK(code, 200, body)
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatalf("export missing UTF-8 BOM")
	}
	if !strings.Contains(body, "name,email,score") {
		t.Fatalf("export header: %q", body[:min(80, len(body))])
	}

	// Unknown route under the resource → 404 JSON.
	code, body = do("GET", "/api/v1/widgets/bogus", "", nil)
	codeOK(code, 404, body)
	if !strings.Contains(body, `"code":"NOT_FOUND"`) {
		t.Fatalf("404 body: %s", body)
	}
	// Method not allowed → 405 JSON.
	code, body = do("PATCH", "/api/v1/widgets/rows", "{}", nil)
	codeOK(code, 405, body)

	// Delete (gate open this time), then the row is gone.
	code, body = do("DELETE", "/api/v1/widgets/rows/"+key, "", nil)
	codeOK(code, 200, body)
	code, body = do("GET", "/api/v1/widgets/rows/"+key, "", nil)
	codeOK(code, 404, body)

	// CRUD sugar path works wholesale through the App mux.
	code, body = do("GET", "/api/data/widgets/rows", "", nil)
	codeOK(code, 200, body)

	// /api/defs lives on the App mux only.
	code, body = do("GET", "/api/defs", "", nil)
	codeOK(code, 200, body)
	var defsList []map[string]any
	if err := json.Unmarshal([]byte(body), &defsList); err != nil {
		t.Fatalf("defs not a list: %s", body)
	}
	var found bool
	for _, d := range defsList {
		if d["name"] == "widgets" {
			found = true
			cols := d["columns"].([]any)
			if len(cols) != 5 {
				t.Fatalf("defs columns = %d, want 5 (introspected set)", len(cols))
			}
			for _, c := range cols {
				cm := c.(map[string]any)
				switch cm["name"] {
				case "name":
					if cm["label"] != "Widget" || cm["required"] != true {
						t.Fatalf("name override not merged: %v", cm)
					}
				case "email":
					if len(cm["validations"].([]any)) != 1 {
						t.Fatalf("email validations not merged: %v", cm)
					}
				case "score":
					if cm["formatting"] == nil {
						t.Fatalf("score formatting not merged: %v", cm)
					}
				case "id":
					if cm["required"] != true {
						t.Fatalf("id (NOT NULL) should default required: %v", cm)
					}
				case "created_at":
					if cm["required"] != false {
						t.Fatalf("nullable created_at should default not-required: %v", cm)
					}
				}
			}
			if p := d["permissions"].(map[string]any); p["delete"] != true {
				t.Fatalf("permissions: %v", p)
			}
		}
	}
	if !found {
		t.Fatalf("widgets def missing from /api/defs: %s", body)
	}
	// Bare resources do NOT serve defs.
	code, body = do("GET", "/api/v1/widgets/defs", "", nil)
	codeOK(code, 404, body)

	// Host middleware really was in front of everything.
	if hits < 15 {
		t.Fatalf("host middleware saw only %d requests", hits)
	}

	// Registration errors: unknown override column, unknown table,
	// unknown hook name (guard), query-def write rejection.
	if _, err := app.Resource("badcols", kucrud.Def{Table: t9Table,
		Columns: []kucrud.Override{{Name: "nope"}}}); err == nil {
		t.Fatal("override on unknown column accepted")
	}
	if _, err := app.Resource("badtable", kucrud.Def{Table: "t9_missing"}); err == nil {
		t.Fatal("unknown table accepted")
	}
	ghost, err := app.Resource("ghosts", kucrud.Def{Table: t9Table,
		Hooks: map[kucrud.Event][]string{kucrud.BeforeCreate: {"no-such-hook"}}})
	if err != nil {
		t.Fatalf("ghost registration should succeed (guard is per-write): %v", err)
	}
	gmux := http.NewServeMux()
	gmux.Handle("/g/", ghost)
	gsrv := httptest.NewServer(gmux)
	t.Cleanup(gsrv.Close)
	gresp, err := http.Post(gsrv.URL+"/g/rows", "application/json",
		strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	gbody, _ := io.ReadAll(gresp.Body)
	gresp.Body.Close()
	if gresp.StatusCode != 400 || !strings.Contains(string(gbody), "HOOK_MISSING") {
		t.Fatalf("ghost hook: %d %s", gresp.StatusCode, gbody)
	}

	// Idempotent re-register replaces the def in the registry.
	if _, err := app.Resource("widgets", kucrud.Def{Table: t9Table, PageSize: 7}); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	code, body = do("GET", "/api/defs", "", nil)
	codeOK(code, 200, body)
	json.Unmarshal([]byte(body), &defsList)
	for _, d := range defsList {
		if d["name"] == "widgets" && d["pageSize"].(float64) != 7 {
			t.Fatalf("re-register did not replace: %v", d["pageSize"])
		}
	}
}

func TestResourceLivePG(t *testing.T)    { runLiveSuite(t, livePG(t)) }
func TestResourceLiveMySQL(t *testing.T) { runLiveSuite(t, liveMySQL(t)) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
