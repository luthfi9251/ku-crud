package api

import (
	"strings"
	"testing"
)

func TestQueryVerifyResync(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedQueryLive(t, s)

	// 1. verify passes on the seeded query
	w := do(s, "GET", "/api/tables/"+tok+"/verify", "", c)
	if w.Code != 200 {
		t.Fatalf("verify = %d %s", w.Code, w.Body)
	}

	// 2. break the stored query → verify 502 (EXPLAIN/plan fails)
	def, cols, _ := s.store.GetTableDef(2)
	def.QuerySQL = "SELECT nope FROM customers"
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	w = do(s, "GET", "/api/tables/"+tok+"/verify", "", c)
	if w.Code != 502 {
		t.Fatalf("broken verify = %d %s", w.Code, w.Body)
	}

	// 3. drift: query adds a column → resync appends it
	def.QuerySQL = "SELECT name AS customer_name, balance, status FROM customers"
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	w = do(s, "GET", "/api/tables/"+tok+"/verify", "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "DRIFT") {
		t.Fatalf("drift verify = %d %s", w.Code, w.Body)
	}
	w = do(s, "POST", "/api/tables/"+tok+"/resync", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "status") {
		t.Fatalf("resync = %d %s", w.Code, w.Body)
	}
	_, fresh, _ := s.store.GetTableDef(2)
	found := false
	for _, fc := range fresh {
		if fc.Name == "status" {
			found = true
		}
	}
	if !found {
		t.Fatal("resync did not append status column")
	}
}
