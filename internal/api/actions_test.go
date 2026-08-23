package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/hooks"
)

// defsEnv: a server with one registered hook and one saved physical-table
// def (id 1) — definition-save tests only, no live database needed.
func defsEnv(t *testing.T) *Server {
	t.Helper()
	reg := hooks.NewRegistry()
	reg.Register("Ping", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		return row, nil
	})
	s := newTestServerHooks(t, reg)
	seedDS(t, s)
	if w := do(s, "POST", "/api/tables", defBody(s), login(s)); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}
	return s
}

// defBodyWithActions splices an actions field into defBody(s).
func defBodyWithActions(s *Server, actions string) string {
	return strings.Replace(defBody(s), `"columns":[`, `"actions":`+actions+`,"columns":[`, 1)
}

// createWithActions saves a def carrying the actions JSON and returns the
// masked id token from the response (for cleanup via DELETE).
func createWithActions(t *testing.T, s *Server, c *string, actions string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	w := do(s, "POST", "/api/tables", defBodyWithActions(s, actions), c)
	var res struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	return res.ID, w
}

func TestTableDefActionsValidation(t *testing.T) {
	s := defsEnv(t)
	c := login(s)
	for _, tc := range []struct {
		actions string
		want    string // expected error substring, "" = save must succeed
	}{
		{`{"hidden":["copy"]}`, ""},
		{`{"hidden":["nope"]}`, "unknown hidden action"},
		{`{"custom":[{"id":"a","label":"A","grant":"read","hook":"Ping","order":1}]}`, ""},
		{`{"custom":[{"id":"a","label":"A","grant":"read","hook":"Ping","order":1},{"id":"a","label":"B","grant":"read","hook":"Ping","order":2}]}`, "duplicate action id"},
		{`{"custom":[{"id":"a","label":"A","grant":"read","hook":"Ghost","order":1}]}`, "not registered"},
		{`{"custom":[{"id":"a","label":"A","grant":"rw","hook":"Ping","order":1}]}`, "grant must be"},
	} {
		id, w := createWithActions(t, s, c, tc.actions)
		if tc.want == "" {
			if w.Code != 200 {
				t.Fatalf("actions %s = %d %s", tc.actions, w.Code, w.Body)
			}
			do(s, "DELETE", "/api/tables/"+id, "", c) // cleanup
		} else if w.Code != 400 || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("actions %s = %d %s", tc.actions, w.Code, w.Body)
		}
	}
}

func TestTableDefActionsDTOEcho(t *testing.T) {
	s := defsEnv(t)
	c := login(s)
	_, w := createWithActions(t, s, c,
		`{"hidden":["export"],"custom":[{"id":"go","label":"Go","grant":"update","hook":"Ping","order":1,"style":"danger"}]}`)
	if w.Code != 200 {
		t.Fatalf("save = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"actions"`) || !strings.Contains(w.Body.String(), `"go"`) {
		t.Fatalf("dto echo = %s", w.Body)
	}
}
