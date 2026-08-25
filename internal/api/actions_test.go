package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ku-crud/internal/engine"
	"ku-crud/internal/hooks"
	"ku-crud/internal/meta"
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

// actionEnv wires a live-PG server with action hooks. seedLive creates
// def 1 (columns incl. name) on the live PG.
func actionEnv(t *testing.T) *Server {
	t.Helper()
	reg := hooks.NewRegistry()
	reg.Register("SayHello", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		row.Message = "hello " + fmt.Sprint(row.Values["name"])
		return row, nil
	})
	reg.Register("Boom", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		return row, errors.New("kaput")
	})
	reg.Register("Panicky", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		panic("kaboom")
	})
	s := newTestServerHooks(t, reg)
	seedLive(t, s)
	return s
}

// setActions writes actions JSON onto a stored def directly (bypasses the
// save-time registry check so tests can simulate drift), like setHooks.
func setActions(t *testing.T, s *Server, defID int64, actionsJSON string) {
	t.Helper()
	def, cols, err := s.store.GetTableDef(defID)
	if err != nil {
		t.Fatal(err)
	}
	def.Actions = actionsJSON
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}

func TestActionHappyPathAndAudit(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := actionEnv(t)
	c := login(s)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":70,"name":"nia"}`, c)
	setActions(t, s, 1, `{"custom":[{"id":"hello","label":"Say hello","grant":"update","hook":"SayHello","order":1}]}`)
	w := do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"70"})+"/action",
		`{"actionId":"hello"}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hello nia") {
		t.Fatalf("action = %d %s", w.Code, w.Body)
	}
	entries, _, _ := s.store.ListAudit(meta.AuditFilter{TableDefID: 1, Limit: 50})
	found := false
	for _, e := range entries {
		if e.Action == "ACTION" && strings.Contains(string(e.NewValues), `"actionId":"hello"`) && e.RowPK != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("no ACTION audit entry")
	}
}

func TestActionGrantGate(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := actionEnv(t)
	c := login(s)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":71,"name":"mia"}`, c)
	setActions(t, s, 1, `{"custom":[{"id":"hello","label":"Say hello","grant":"update","hook":"SayHello","order":1}]}`)
	reader := loginAs(t, s, "areader", &meta.Role{Name: "AReader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	writer := loginAs(t, s, "awriter", &meta.Role{Name: "AWriter"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true, CanUpdate: true}})
	url := "/api/tables/" + tdTok(s, 1) + "/rows/" + engine.EncodeRowKey([]string{"71"}) + "/action"
	if w := do(s, "POST", url, `{"actionId":"hello"}`, reader); w.Code != 403 {
		t.Fatalf("reader = %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", url, `{"actionId":"hello"}`, writer); w.Code != 200 {
		t.Fatalf("writer = %d %s", w.Code, w.Body)
	}
	// read-grant action is open to the reader
	setActions(t, s, 1, `{"custom":[{"id":"peek","label":"Peek","grant":"read","hook":"SayHello","order":1}]}`)
	if w := do(s, "POST", url, `{"actionId":"peek"}`, reader); w.Code != 200 {
		t.Fatalf("reader peek = %d %s", w.Code, w.Body)
	}
}

func TestActionNotFoundRowMissing(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := actionEnv(t)
	c := login(s)
	setActions(t, s, 1, `{"custom":[{"id":"hello","label":"Say hello","grant":"read","hook":"SayHello","order":1}]}`)
	tok := tdTok(s, 1)
	w := do(s, "POST", "/api/tables/"+tok+"/rows/"+engine.EncodeRowKey([]string{"1"})+"/action",
		`{"actionId":"nope"}`, c)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "ACTION_NOT_FOUND") {
		t.Fatalf("unknown = %d %s", w.Code, w.Body)
	}
	w = do(s, "POST", "/api/tables/"+tok+"/rows/"+engine.EncodeRowKey([]string{"999"})+"/action",
		`{"actionId":"hello"}`, c)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "NOT_FOUND") {
		t.Fatalf("row missing = %d %s", w.Code, w.Body)
	}
}

func TestActionHookMissingAndFailure(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := actionEnv(t)
	c := login(s)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":72,"name":"jo"}`, c)
	url := "/api/tables/" + tdTok(s, 1) + "/rows/" + engine.EncodeRowKey([]string{"72"}) + "/action"
	setActions(t, s, 1, `{"custom":[{"id":"g","label":"G","grant":"read","hook":"Ghost","order":1}]}`)
	if w := do(s, "POST", url, `{"actionId":"g"}`, c); w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_MISSING") {
		t.Fatalf("missing = %d %s", w.Code, w.Body)
	}
	setActions(t, s, 1, `{"custom":[{"id":"b","label":"B","grant":"read","hook":"Boom","order":1}]}`)
	if w := do(s, "POST", url, `{"actionId":"b"}`, c); w.Code != 400 || !strings.Contains(w.Body.String(), "ACTION_FAILED") || !strings.Contains(w.Body.String(), "kaput") {
		t.Fatalf("boom = %d %s", w.Code, w.Body)
	}
	// failed run is still audited (status=error)
	entries, _, _ := s.store.ListAudit(meta.AuditFilter{TableDefID: 1, Limit: 50})
	found := false
	for _, e := range entries {
		if e.Action == "ACTION" && strings.Contains(string(e.NewValues), `"status":"error"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("failed action not audited")
	}
	setActions(t, s, 1, `{"custom":[{"id":"p","label":"P","grant":"read","hook":"Panicky","order":1}]}`)
	if w := do(s, "POST", url, `{"actionId":"p"}`, c); w.Code != 400 || !strings.Contains(w.Body.String(), "ACTION_FAILED") {
		t.Fatalf("panic = %d %s", w.Code, w.Body)
	}
}

func TestActionQueryViewRejected(t *testing.T) {
	s := newTestServer(t)
	seedQueryDef(t, s, []string{"n"})
	stranger := loginAs(t, s, "qact", &meta.Role{Name: "QAct"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	setActions(t, s, 1, `{"custom":[{"id":"x","label":"X","grant":"read","hook":"H","order":1}]}`)
	w := do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows/"+rowKeyToken([]string{"jo"})+"/action",
		`{"actionId":"x"}`, stranger)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "QUERY_READONLY") {
		t.Fatalf("query action = %d %s", w.Code, w.Body)
	}
}
