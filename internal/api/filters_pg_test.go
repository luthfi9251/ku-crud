package api

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestFiltersPG(t *testing.T) {
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s) // def 1: customers(name text, ...) — see rows_test.go:14
	tok := tdTok(s, 1)

	// 1. filtered list returns 200 and a sane total
	f := url.QueryEscape(`[{"column":"name","op":"contains","values":["o"]}]`)
	w := do(s, "GET", "/api/tables/"+tok+"/rows?filters="+f, "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var res struct {
		Total int `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total < 1 || res.Total > 3 {
		t.Fatalf("filtered total = %d", res.Total)
	}

	// 2. export honors the same filter (row count == filtered total)
	body, _ := exportCSV(t, s, tok, "?filters="+f, *c)
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines)-1 != res.Total {
		t.Fatalf("export rows %d != filtered total %d", len(lines)-1, res.Total)
	}

	// 3. unfiltered export is a superset
	bodyAll, _ := exportCSV(t, s, tok, "", *c)
	linesAll := strings.Split(strings.TrimSpace(bodyAll), "\n")
	if len(linesAll)-1 < res.Total {
		t.Fatalf("unfiltered %d < filtered %d", len(linesAll)-1, res.Total)
	}

	// 4. invalid filter -> 400 FILTER_INVALID
	w = do(s, "GET", "/api/tables/"+tok+"/rows?filters="+
		url.QueryEscape(`[{"column":"hax","op":"eq","values":["1"]}]`), "", c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "FILTER_INVALID") {
		t.Fatalf("bad filter = %d %s", w.Code, w.Body)
	}
	// 5. injection payload in column name -> 400, never reaches SQL
	w = do(s, "GET", "/api/tables/"+tok+"/rows?filters="+
		url.QueryEscape(`[{"column":"name; DROP TABLE x","op":"eq","values":["1"]}]`), "", c)
	if w.Code != 400 {
		t.Fatalf("injection filter = %d %s", w.Code, w.Body)
	}
}

// TestFiltersFKPG filters the child table by the parent's display value via
// the fk LEFT JOIN (seedFKLive: orders.customer_id → customers name/city).
func TestFiltersFKPG(t *testing.T) {
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s) // defs: customers=1, orders=2 (fk customer_id)
	tok := tdTok(s, 2)

	// contains "joe" matches only customer 'joe' → order o2
	f := url.QueryEscape(`[{"column":"customer_id","op":"contains","values":["joe"]}]`)
	w := do(s, "GET", "/api/tables/"+tok+"/rows?filters="+f, "", c)
	if w.Code != 200 {
		t.Fatalf("fk filtered list = %d %s", w.Code, w.Body)
	}
	var res struct {
		Total int              `json:"total"`
		Rows  []map[string]any `json:"rows"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 1 || len(res.Rows) != 1 || res.Rows[0]["note"] != "o2" {
		t.Fatalf("fk filtered total=%d rows=%+v", res.Total, res.Rows)
	}

	// contains "jo" matches customers 'jo' and 'joe' → orders o1, o2
	f = url.QueryEscape(`[{"column":"customer_id","op":"contains","values":["jo"]}]`)
	w = do(s, "GET", "/api/tables/"+tok+"/rows?filters="+f, "", c)
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Total != 2 {
		t.Fatalf("fk contains jo = %d %s (total=%d)", w.Code, w.Body, res.Total)
	}
}
