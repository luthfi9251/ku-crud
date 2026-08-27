package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	kucrud "github.com/luthfi9251/ku-crud/core"
	"github.com/luthfi9251/ku-crud/core/engine"

	"kucrud-template/authstub"
)

// sqlOpen opens a raw handle on the test connection (the pgx stdlib
// driver registers through the library's import chain).
func sqlOpen(c kucrud.Conn) (*sql.DB, error) {
	db, err := sql.Open("pgx", pgDSN(c))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// The smoke test exercises the shipped stack end-to-end. It needs a live
// PostgreSQL because kucrud.New validates the connection and CRUD
// registration introspects the tables — self-skip without the env.
func livePG(t *testing.T) kucrud.Conn {
	t.Helper()
	dsn := os.Getenv("KUCRUD_TEST_PG")
	if dsn == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	return kucrud.Conn{Driver: "postgres", Raw: dsn}
}

func resetTables(t *testing.T, c kucrud.Conn) {
	t.Helper()
	db, err := sqlOpen(c)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	for _, q := range []string{"DELETE FROM products", "DELETE FROM categories"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("reset %q: %v", q, err)
		}
	}
	t.Cleanup(func() {
		db, err := sqlOpen(c)
		if err != nil {
			return
		}
		defer db.Close()
		db.Exec("DELETE FROM products")
		db.Exec("DELETE FROM categories")
	})
}

func seedCategory(t *testing.T, c kucrud.Conn) int {
	t.Helper()
	db, err := sqlOpen(c)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var id int
	if err := db.QueryRow("INSERT INTO categories (name) VALUES ($1) RETURNING id", "TestCat").Scan(&id); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	return id
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
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
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func wantStatus(t *testing.T, got, want int, body string) {
	t.Helper()
	if got != want {
		t.Fatalf("status %d, want %d (body %s)", got, want, body)
	}
}

// TestSmokeStubGateDeniesAll proves the starter is deny-all as shipped:
// every data route answers 403 before touching a row, and /api/defs
// reports no permissions.
func TestSmokeStubGateDeniesAll(t *testing.T) {
	c := livePG(t)
	app, err := newApp(c, authstub.Gate)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	t.Cleanup(func() { app.Close() })
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/data/products/rows", ""},
		{"POST", "/api/data/products/rows", `{"name":"x"}`},
		{"DELETE", "/api/data/products/rows/whatever", ""},
		{"GET", "/api/data/categories/rows", ""},
	} {
		code, body := do(t, srv, tc.method, tc.path, tc.body)
		wantStatus(t, code, 403, body)
		if !strings.Contains(body, `"code":"FORBIDDEN"`) ||
			!strings.Contains(body, "auth not configured") {
			t.Fatalf("stub gate body: %s", body)
		}
	}

	code, body := do(t, srv, "GET", "/api/defs", "")
	wantStatus(t, code, 200, body)
	var defs []map[string]any
	if err := json.Unmarshal([]byte(body), &defs); err != nil {
		t.Fatalf("defs not a list: %s", body)
	}
	for _, d := range defs {
		p := d["permissions"].(map[string]any)
		for _, op := range []string{"read", "create", "update", "delete"} {
			if p[op] == true {
				t.Fatalf("deny-all stub must report no permissions: %v", d["name"])
			}
		}
	}
}

// TestSmokeAllowAllRoundtrip replaces the stub with an allow-all gate
// and drives one full CRUD roundtrip through the shipped routes,
// proving the stack works end-to-end once auth allows it.
func TestSmokeAllowAllRoundtrip(t *testing.T) {
	c := livePG(t)
	resetTables(t, c)
	catID := seedCategory(t, c)

	allowAll := func(r *http.Request, op kucrud.Op, table string) error { return nil }
	app, err := newApp(c, allowAll)
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	t.Cleanup(func() { app.Close() })
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)

	// Create.
	code, body := do(t, srv, "POST", "/api/data/products/rows",
		fmt.Sprintf(`{"name":"Coffee","price":4.5,"category_id":%d}`, catID))
	wantStatus(t, code, 200, body)
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create body: %s", body)
	}

	// List: the row is there with its fk relation resolved.
	code, body = do(t, srv, "GET", "/api/data/products/rows", "")
	wantStatus(t, code, 200, body)
	var list struct {
		Rows  []map[string]any                     `json:"rows"`
		Total float64                              `json:"total"`
		Rels  map[string]map[string]map[string]any `json:"rels"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("list body: %s", body)
	}
	if list.Total != 1 || len(list.Rows) != 1 {
		t.Fatalf("list: total=%v rows=%d", list.Total, len(list.Rows))
	}
	row := list.Rows[0]
	if row["name"] != "Coffee" {
		t.Fatalf("row: %v", row)
	}
	rel, ok := list.Rels["category_id"][fmt.Sprint(row["category_id"])]
	if !ok || rel["name"] != "TestCat" {
		t.Fatalf("fk rel not resolved: %v", list.Rels)
	}

	// Defs: the declaration's overrides are visible to the web app.
	code, body = do(t, srv, "GET", "/api/defs", "")
	wantStatus(t, code, 200, body)
	if !strings.Contains(body, `"formatting":{"number":{"decimals":2}}`) ||
		!strings.Contains(body, `"fieldType":"fk"`) {
		t.Fatalf("defs missing overrides: %s", body)
	}

	// FK picker target resolves through the registered categories def.
	code, body = do(t, srv, "GET", "/api/data/products/fkoptions/category_id", "")
	wantStatus(t, code, 200, body)
	if !strings.Contains(body, "TestCat") {
		t.Fatalf("fkoptions: %s", body)
	}

	// Update via the encoded row key, then read it back.
	key := engine.EncodeRowKey([]string{fmt.Sprint(row["id"])})
	code, body = do(t, srv, "PUT", "/api/data/products/rows/"+key, `{"name":"Espresso"}`)
	wantStatus(t, code, 200, body)
	code, body = do(t, srv, "GET", "/api/data/products/rows/"+key, "")
	wantStatus(t, code, 200, body)
	if !strings.Contains(body, `"Espresso"`) {
		t.Fatalf("update not applied: %s", body)
	}

	// Delete, then the row is gone.
	code, body = do(t, srv, "DELETE", "/api/data/products/rows/"+key, "")
	wantStatus(t, code, 200, body)
	code, body = do(t, srv, "GET", "/api/data/products/rows/"+key, "")
	wantStatus(t, code, 404, body)
}
