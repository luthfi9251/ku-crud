package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestCSVCell(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{true, "true"},
		{false, "false"},
		{"plain", "plain"},
		{[]byte("bytes"), "bytes"},
		{float64(10.5), "10.5"},
		{float64(2), "2"},
		{int64(7), "7"},
	}
	for _, c := range cases {
		if got := csvCell(c.in); got != c.want {
			t.Errorf("csvCell(%v) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestJoinDisplay(t *testing.T) {
	rel := map[string]any{"id": 3.0, "name": "jo", "email": "jo@x.io"}
	if got := joinDisplay(rel, []string{"name", "email"}, "id"); got != "jo — jo@x.io" {
		t.Fatalf("joinDisplay = %q", got)
	}
	// no display columns configured → ref column value
	if got := joinDisplay(rel, nil, "id"); got != "3" {
		t.Fatalf("fallback = %q", got)
	}
}

// exportCSV runs the export endpoint and returns the body with the BOM
// stripped, plus the raw first three bytes for BOM assertions.
func exportCSV(t *testing.T, s *Server, tok, query, cookie string) (body string, hasBOM bool) {
	t.Helper()
	w := do(s, "GET", "/api/tables/"+tok+"/rows/export"+query, "", &cookie)
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	b := w.Body.String()
	if strings.HasPrefix(b, "\xEF\xBB\xBF") {
		return b[3:], true
	}
	return b, false
}

func TestExportCSV(t *testing.T) {
	s := newTestServer(t)
	c := *login(s)
	seedLive(t, s)
	tok := tdTok(s, 1)

	body, bom := exportCSV(t, s, tok, "", c)
	if !bom {
		t.Fatal("UTF-8 BOM missing")
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 4 { // header + 3 rows
		t.Fatalf("lines = %d: %q", len(lines), body)
	}
	if lines[0] != "id,name,active,balance,born,status" {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(body, ",jo,true,10.50,,sunny") || !strings.Contains(body, ",ana,true,0.00,") {
		t.Fatalf("data rows wrong: %q", body)
	}

	// search is applied
	body, _ = exportCSV(t, s, tok, "?search=jo", c)
	lines = strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 3 { // header + jo + joe
		t.Fatalf("search export = %q", body)
	}
	if !strings.Contains(body, "joe") || strings.Contains(body, "ana") {
		t.Fatalf("search filter wrong: %q", body)
	}

	// sort DESC by name: joe before jo
	body, _ = exportCSV(t, s, tok, "?sort=name&dir=DESC", c)
	if strings.Index(body, "joe") > strings.Index(body, ",jo,") {
		t.Fatalf("sort not applied: %q", body)
	}

	// quoting: a value with comma/quote/newline is quoted properly (create one)
	w := do(s, "POST", "/api/tables/"+tok+"/rows", `{"name":"a,b \"q\"\nc","status":"sunny"}`, &c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	body, _ = exportCSV(t, s, tok, "", c)
	if !strings.Contains(body, `"a,b ""q""`+"\n"+`c"`) {
		t.Fatalf("csv quoting = %q", body)
	}
}

func TestExportCSVFKDisplay(t *testing.T) {
	// orders.customer_id fk → customers display (name, city)
	s := newTestServer(t)
	c := *login(s)
	seedFKLive(t, s)

	body, _ := exportCSV(t, s, tdTok(s, 2), "", c)
	if !strings.Contains(body, "jo — Bandung") || !strings.Contains(body, "joe — Jakarta") {
		t.Fatalf("fk display in export: %q", body)
	}
	if !strings.Contains(body, "customer_id") {
		t.Fatalf("header = %q", body)
	}
}

func TestExportCSVGrants(t *testing.T) {
	s := newTestServer(t)
	seedLive(t, s)
	tok := tdTok(s, 1)

	// read grant → 200
	reader := loginAs(t, s, "reader", &meta.Role{Name: "Reader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	if w := do(s, "GET", "/api/tables/"+tok+"/rows/export", "", reader); w.Code != 200 {
		t.Fatalf("reader export = %d %s", w.Code, w.Body)
	}
	// no grant → 403
	noGrant := loginAs(t, s, "nogrant", &meta.Role{Name: "NoGrant"}, nil)
	if w := do(s, "GET", "/api/tables/"+tok+"/rows/export", "", noGrant); w.Code != 403 {
		t.Fatalf("no-grant export = %d %s", w.Code, w.Body)
	}
}
