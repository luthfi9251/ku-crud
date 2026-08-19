package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRowWriteAndAudit(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)

	// CREATE
	w := do(s, "POST", "/api/tables/1/rows",
		`{"name":"nia","active":false,"balance":7.25,"born":"1990-01-02","status":"rainy"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}

	// CREATE with unknown column → VALIDATION
	if w = do(s, "POST", "/api/tables/1/rows", `{"name":"x","hax":1}`, c); w.Code != 400 {
		t.Fatalf("unknown col = %d %s", w.Code, w.Body)
	}
	// CREATE missing required (name) → VALIDATION
	if w = do(s, "POST", "/api/tables/1/rows", `{"active":true}`, c); w.Code != 400 {
		t.Fatalf("missing required = %d %s", w.Code, w.Body)
	}
	// bad enum → VALIDATION
	if w = do(s, "POST", "/api/tables/1/rows", `{"name":"y","status":"snowy"}`, c); w.Code != 400 {
		t.Fatalf("bad enum = %d %s", w.Code, w.Body)
	}
	// bad datetime → VALIDATION
	if w = do(s, "POST", "/api/tables/1/rows", `{"name":"y","born":"02/01/1990"}`, c); w.Code != 400 {
		t.Fatalf("bad datetime = %d %s", w.Code, w.Body)
	}
	// non-editable column (id) in payload → VALIDATION
	if w = do(s, "POST", "/api/tables/1/rows", `{"name":"y","id":9}`, c); w.Code != 400 {
		t.Fatalf("non-editable col = %d %s", w.Code, w.Body)
	}

	// UPDATE row 1
	w = do(s, "PUT", "/api/tables/1/rows/"+encodeRowKey([]string{"1"}), `{"name":"jo!"}`, c)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/tables/1/rows/"+encodeRowKey([]string{"1"}), "", c); !strings.Contains(w.Body.String(), `"name":"jo!"`) {
		t.Fatalf("row after update = %s", w.Body)
	}

	// DELETE row 4 (nia)
	if w = do(s, "DELETE", "/api/tables/1/rows/"+encodeRowKey([]string{"4"}), "", c); w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/tables/1/rows/"+encodeRowKey([]string{"4"}), "", c); w.Code != 404 {
		t.Fatalf("deleted row still there = %d", w.Code)
	}

	// audit: 1 INSERT + 1 UPDATE + 1 DELETE
	w = do(s, "GET", "/api/audit?tableDefId=1", "", c)
	if w.Code != 200 {
		t.Fatalf("audit = %d %s", w.Code, w.Body)
	}
	var res struct {
		Entries []struct {
			Action    string          `json:"action"`
			RowPK     string          `json:"rowPk"`
			OldValues json.RawMessage `json:"oldValues"`
			NewValues json.RawMessage `json:"newValues"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 3 {
		t.Fatalf("audit total=%d body=%s", res.Total, w.Body)
	}
	byAction := map[string]json.RawMessage{}
	for _, e := range res.Entries {
		// INSERT asserts new values; UPDATE/DELETE assert old values.
		v := e.OldValues
		if e.Action == "INSERT" {
			v = e.NewValues
		}
		byAction[e.Action] = v
	}
	if string(byAction["INSERT"]) == "null" {
		t.Fatal("INSERT audit must carry new values")
	}
	if string(byAction["UPDATE"]) == "null" || !strings.Contains(string(byAction["UPDATE"]), `"jo"`) {
		t.Fatalf("UPDATE audit must carry old values: %s", byAction["UPDATE"])
	}
	if string(byAction["DELETE"]) == "null" || !strings.Contains(string(byAction["DELETE"]), `"nia"`) {
		t.Fatalf("DELETE audit must carry old values: %s", byAction["DELETE"])
	}
}
