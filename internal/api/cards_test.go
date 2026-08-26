package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func seedCardsDef(t *testing.T, s *Server) string {
	t.Helper()
	seedDS(t, s)
	body := `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"orders3",
"label":"Orders3","keyColumns":["id"],"pageSize":20,"columns":[
 {"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,
  "visible":true,"searchable":true,"sortable":true,"position":0},
 {"name":"amount","label":"Amount","fieldType":"number","editable":true,
  "visible":true,"position":1},
 {"name":"status","label":"Status","fieldType":"enum","enumOptions":["open","paid"],
  "editable":true,"visible":true,"position":2}]}`
	if w := do(s, "POST", "/api/tables", body, login(s)); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}
	return tdTok(s, 1)
}

func TestCardsAdminCRUD(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedCardsDef(t, s)

	w := do(s, "POST", "/api/cards", `{"tableDefId":"`+tok+`","label":"Revenue","func":"sum","column":"amount","filters":"[]"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var card struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &card)
	if card.ID == "" || card.ID == "1" {
		t.Fatalf("card id not masked: %q", card.ID)
	}

	w = do(s, "GET", "/api/cards", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["tableName"] != "orders3" || list[0]["tableDefId"] != tok {
		t.Fatalf("list = %s", w.Body)
	}

	w = do(s, "PUT", "/api/cards/"+card.ID, `{"tableDefId":"`+tok+`","label":"Paid revenue","func":"sum","column":"amount",
"filters":"[{\"column\":\"status\",\"op\":\"eq\",\"values\":[\"paid\"]}]"}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Paid revenue") {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}

	// two cards: moving the second one up now has a neighbor
	do(s, "POST", "/api/cards", `{"tableDefId":"`+tok+`","label":"Count","func":"count","filters":"[]"}`, c)
	w = do(s, "GET", "/api/cards", "", c)
	var both []struct {
		ID   string `json:"id"`
		Label string `json:"label"`
	}
	json.Unmarshal(w.Body.Bytes(), &both)
	if len(both) != 2 {
		t.Fatalf("two cards expected: %s", w.Body)
	}
	if both[0].Label != "Paid revenue" {
		t.Fatalf("order before move = %+v", both)
	}
	w = do(s, "POST", "/api/cards/"+both[1].ID+"/move", `{"dir":"up"}`, c)
	if w.Code != 200 {
		t.Fatalf("move = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/cards", "", c)
	json.Unmarshal(w.Body.Bytes(), &both)
	if both[0].Label != "Count" {
		t.Fatalf("order after move = %+v", both)
	}
	w = do(s, "POST", "/api/cards/"+both[0].ID+"/move", `{"dir":"up"}`, c)
	if w.Code != 400 {
		t.Fatalf("top move up = %d %s", w.Code, w.Body)
	}

	w = do(s, "DELETE", "/api/cards/"+card.ID, "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
}

func TestCardsValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedCardsDef(t, s)
	for _, body := range []string{
		`{"tableDefId":"` + tok + `","label":"X","func":"median","column":"amount","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"sum","column":"status","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"count","column":"amount","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"sum","column":"","filters":"[]"}`,
		`{"tableDefId":"` + tok + `","label":"X","func":"count","filters":"[{\"column\":\"nope\",\"op\":\"eq\",\"values\":[\"x\"]}]"}`,
		`{"tableDefId":"zzzzzzzzzzz","label":"X","func":"count","filters":"[]"}`,
	} {
		w := do(s, "POST", "/api/cards", body, c)
		if w.Code != 400 {
			t.Fatalf("accepted bad card: %d %s", w.Code, body)
		}
	}
}

func TestCardsRBAC(t *testing.T) {
	s := newTestServer(t)
	tok := seedCardsDef(t, s)
	admin := login(s)
	if w := do(s, "POST", "/api/cards", `{"tableDefId":"`+tok+`","label":"Revenue","func":"sum","column":"amount","filters":"[]"}`, admin); w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}

	reader := loginAs(t, s, "reader", &meta.Role{Name: "Reader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	stranger := loginAs(t, s, "stranger", &meta.Role{Name: "Stranger"}, nil)

	// reader (read grant) sees the card
	w := do(s, "GET", "/api/cards", "", reader)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Revenue") {
		t.Fatalf("reader list = %d %s", w.Code, w.Body)
	}
	// stranger (no grant) does not
	w = do(s, "GET", "/api/cards", "", stranger)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Revenue") {
		t.Fatalf("stranger list = %d %s", w.Code, w.Body)
	}
	// non-admin cannot manage
	for _, p := range []struct{ method, path string }{
		{"POST", "/api/cards"}, {"PUT", "/api/cards/x"}, {"DELETE", "/api/cards/x"}, {"POST", "/api/cards/x/move"},
	} {
		if w := do(s, p.method, p.path, `{}`, reader); w.Code != 403 {
			t.Fatalf("%s %s reader = %d %s", p.method, p.path, w.Code, w.Body)
		}
	}
}
