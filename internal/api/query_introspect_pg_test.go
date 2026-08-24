package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestQueryIntrospect(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s) // ds id 2 = live
	dsTok := s.ids.Encode("ds", 2)

	w := do(s, "POST", "/api/datasources/"+dsTok+"/query-introspect",
		`{"query":"SELECT name AS n, balance, 1+1 FROM customers"}`, c)
	if w.Code != 200 {
		t.Fatalf("introspect = %d %s", w.Code, w.Body)
	}
	var res struct {
		Columns []struct {
			Name      string `json:"name"`
			FieldType string `json:"fieldType"`
		} `json:"columns"`
		Dropped []string `json:"dropped"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Columns) != 2 || res.Columns[0].Name != "n" || len(res.Dropped) != 1 {
		t.Fatalf("res = %s", w.Body)
	}

	w = do(s, "POST", "/api/datasources/"+dsTok+"/query-introspect",
		`{"query":"DELETE FROM customers"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "QUERY_INVALID") {
		t.Fatalf("bad query = %d %s", w.Code, w.Body)
	}
}
