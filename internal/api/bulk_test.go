package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestBulkDelete(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s) // customers 1-3, orders reference customers 1 & 2
	ordTok := tdTok(s, 2)
	custTok := tdTok(s, 1)

	// delete 2 orders (no inbound references) → both gone, audited
	keys := encodeRowKey([]string{"1"}) + `","` + encodeRowKey([]string{"2"})
	w := do(s, "POST", "/api/tables/"+ordTok+"/rows/bulk-delete", `{"keys":["`+keys+`"]}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":2`) || !strings.Contains(w.Body.String(), `"failed":0`) {
		t.Fatalf("bulk orders = %d %s", w.Code, w.Body)
	}
	// audit: 2 DELETE entries for orders
	w = do(s, "GET", "/api/audit?tableDefId="+ordTok, "", c)
	if strings.Count(w.Body.String(), `"DELETE"`) != 2 {
		t.Fatalf("audit deletes = %s", w.Body)
	}

	// customers: 1 & 2 are referenced? orders 1 & 2 just deleted, so only the
	// rows themselves remain; delete 1 (ok), 2 (ok), 999 (not found)
	keys = encodeRowKey([]string{"1"}) + `","` + encodeRowKey([]string{"2"}) + `","` + encodeRowKey([]string{"999"})
	w = do(s, "POST", "/api/tables/"+custTok+"/rows/bulk-delete", `{"keys":["`+keys+`"]}`, c)
	if w.Code != 200 {
		t.Fatalf("bulk customers = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"deleted":2`) || !strings.Contains(w.Body.String(), `"failed":1`) {
		t.Fatalf("counts = %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "NOT_FOUND") {
		t.Fatalf("failure detail = %s", w.Body)
	}
}

func TestBulkDeleteConflictPartialSuccess(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s)
	ordTok := tdTok(s, 2)
	custTok := tdTok(s, 1)

	// delete customer 2 (referenced by order 2 → CONFLICT) and customer 3 (ok)
	keys := encodeRowKey([]string{"2"}) + `","` + encodeRowKey([]string{"3"})
	w := do(s, "POST", "/api/tables/"+custTok+"/rows/bulk-delete", `{"keys":["`+keys+`"]}`, c)
	if w.Code != 200 {
		t.Fatalf("bulk = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"deleted":1`) || !strings.Contains(w.Body.String(), `"failed":1`) {
		t.Fatalf("counts = %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "CONFLICT") || !strings.Contains(w.Body.String(), "Orders") {
		t.Fatalf("conflict detail = %s", w.Body)
	}
	// customer 3 really deleted, customer 2 still there
	w = do(s, "GET", "/api/tables/"+custTok+"/rows", "", c)
	if !strings.Contains(w.Body.String(), `"total":2`) || !strings.Contains(w.Body.String(), `"joe"`) {
		t.Fatalf("remaining rows = %s", w.Body)
	}
	_ = ordTok
}

func TestBulkDeleteGrantsAndCaps(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)
	tok := tdTok(s, 1)

	// no delete grant → 403
	reader := loginAs(t, s, "deleter", &meta.Role{Name: "Reader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	if w := do(s, "POST", "/api/tables/"+tok+"/rows/bulk-delete", `{"keys":["`+encodeRowKey([]string{"1"})+`"]}`, reader); w.Code != 403 {
		t.Fatalf("no-grant bulk = %d %s", w.Code, w.Body)
	}
	// cap → 400
	var many []string
	for i := 0; i < 1001; i++ {
		many = append(many, encodeRowKey([]string{"1"}))
	}
	body := `{"keys":["` + strings.Join(many, `","`) + `"]}`
	if w := do(s, "POST", "/api/tables/"+tok+"/rows/bulk-delete", body, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "BULK_TOO_LARGE") {
		t.Fatalf("cap = %d %s", w.Code, w.Body)
	}
	// empty keys → 400
	if w := do(s, "POST", "/api/tables/"+tok+"/rows/bulk-delete", `{"keys":[]}`, c); w.Code != 400 {
		t.Fatalf("empty = %d", w.Code)
	}
}
