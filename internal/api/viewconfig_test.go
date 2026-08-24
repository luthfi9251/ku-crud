package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestViewConfigRoundtripAndReject(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	var m map[string]any
	if err := json.Unmarshal([]byte(defBody(s)), &m); err != nil {
		t.Fatal(err)
	}
	// visible text column "name": kanban card title + calendar rejection target
	m["columns"] = append(m["columns"].([]any), map[string]any{
		"name": "name", "label": "Name", "fieldType": "text",
		"editable": true, "required": false, "visible": true,
		"searchable": true, "sortable": true, "position": 2,
	})
	m["defaultView"] = "kanban"
	m["viewConfig"] = json.RawMessage(`{"kanbanBoardColumn":"status","kanbanDisplayColumn":"name"}`)
	w := do(s, "POST", "/api/tables", string(mustJSON(m)), c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"defaultView":"kanban"`) ||
		!strings.Contains(w.Body.String(), `"kanbanBoardColumn":"status"`) {
		t.Fatalf("view config not roundtripped: %s", w.Body)
	}

	// persisted — Get SELECT returns it
	w = do(s, "GET", "/api/tables/"+s.ids.Encode("td", 1), "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kanbanBoardColumn":"status"`) {
		t.Fatalf("view config not persisted: %d %s", w.Code, w.Body)
	}
	// Update UPDATE keeps it
	w = do(s, "PUT", "/api/tables/"+s.ids.Encode("td", 1), string(mustJSON(m)), c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"defaultView":"kanban"`) {
		t.Fatalf("view config lost on update: %d %s", w.Code, w.Body)
	}

	m["defaultView"] = "calendar"
	m["viewConfig"] = json.RawMessage(`{"calendarStartColumn":"name"}`) // name is text, not datetime
	w = do(s, "POST", "/api/tables", string(mustJSON(m)), c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "datetime") {
		t.Fatalf("bad calendar config not rejected: %d %s", w.Code, w.Body)
	}
}
