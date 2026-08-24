package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestCheckFormatting(t *testing.T) {
	good := func(raw string, fieldType string) {
		t.Helper()
		if msg := checkFormatting(meta.ColumnDef{Name: "x", FieldType: fieldType, Formatting: raw}); msg != "" {
			t.Fatalf("%s: unexpected reject %s", raw, msg)
		}
	}
	bad := func(raw string, fieldType string, wantSub string) {
		t.Helper()
		msg := checkFormatting(meta.ColumnDef{Name: "x", FieldType: fieldType, Formatting: raw})
		if msg == "" {
			t.Fatalf("%s: expected rejection", raw)
		}
		if msg != "" && !strings.Contains(msg, wantSub) {
			t.Fatalf("%s: msg %q want substring %q", raw, msg, wantSub)
		}
	}
	good(`{"number":{"thousands":true,"decimals":2,"prefix":"Rp "}}`, "number")
	good(`{"enumColors":{"a":"green"}}`, "enum")
	bad(`{"enumColors":{"a":"neon"}}`, "enum", "unknown enum color")
	bad(`{"enumColors":{"a":"green"}}`, "number", "requires an enum column")
	bad(`{"number":{"decimals":9}}`, "number", "decimals must be 0..6")
	bad(`not json`, "number", "not valid JSON")
}

func TestFormattingRoundtrip(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	raw := `{"number":{"thousands":true,"decimals":2,"prefix":"Rp "}}`
	var m map[string]any
	if err := json.Unmarshal([]byte(defBody(s)), &m); err != nil {
		t.Fatal(err)
	}
	col := m["columns"].([]any)[0].(map[string]any)
	col["fieldType"] = "number"
	col["formatting"] = json.RawMessage(raw)
	w := do(s, "POST", "/api/tables", string(mustJSON(m)), c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"formatting":{"number":{"thousands":true,"decimals":2,"prefix":"Rp "}}`) {
		t.Fatalf("formatting not roundtripped: %s", w.Body)
	}
}
