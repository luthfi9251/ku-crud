package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/luthfi9251/ku-crud/core/hooks"
	"ku-crud/internal/meta"
)

// newTestServerHooks mirrors newTestServer (auth_test.go) but injects a
// hook registry. reg may be nil.
func newTestServerHooks(t *testing.T, reg *hooks.Registry) *Server {
	t.Helper()
	store, err := meta.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.CreateUser("alice", "secret"); err != nil {
		t.Fatal(err)
	}
	srv, err := New(store, reg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestHookListEndpoint(t *testing.T) {
	reg := hooks.NewRegistry()
	reg.Register("Alpha", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		return row, nil
	})
	s := newTestServerHooks(t, reg)
	c := login(s)
	w := do(s, "GET", "/api/hooks", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var res struct{ Hooks []string }
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Hooks) != 1 || res.Hooks[0] != "Alpha" {
		t.Fatalf("hooks = %v", res.Hooks)
	}
	// nil registry → empty list, still 200
	s2 := newTestServerHooks(t, nil)
	c2 := login(s2)
	if w = do(s2, "GET", "/api/hooks", "", c2); w.Code != 200 {
		t.Fatalf("nil registry list = %d", w.Code)
	}
}

func TestTableDefHooksValidation(t *testing.T) {
	reg := hooks.NewRegistry()
	reg.Register("Good", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		return row, nil
	})
	s := newTestServerHooks(t, reg)
	c := login(s)
	seedDS(t, s)

	body := `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"t",
"label":"T","keyColumns":["id"],"pageSize":20,
"hooks":{"beforeCreate":[{"hook":"Missing","order":1}]},
"columns":[{"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,"visible":true,"searchable":true,"sortable":true,"position":0}]}`
	w := do(s, "POST", "/api/tables", body, c)
	if w.Code != 400 {
		t.Fatalf("unknown hook must 400, got %d %s", w.Code, w.Body)
	}

	body = `{"datasourceId":"` + s.ids.Encode("ds", 1) + `","schemaName":"public","tableName":"t",
"label":"T","keyColumns":["id"],"pageSize":20,
"hooks":{"beforeCreate":[{"hook":"Good","config":{"x":1},"order":1}]},
"columns":[{"name":"id","label":"ID","fieldType":"number","editable":true,"required":true,"visible":true,"searchable":true,"sortable":true,"position":0}]}`
	if w = do(s, "POST", "/api/tables", body, c); w.Code != 200 {
		t.Fatalf("valid hooks = %d %s", w.Code, w.Body)
	}
	// GET returns hooks as a JSON object
	w = do(s, "GET", "/api/tables/"+tdTok(s, 1), "", c)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var def struct{ Hooks map[string][]json.RawMessage }
	json.Unmarshal(w.Body.Bytes(), &def)
	if len(def.Hooks["beforeCreate"]) != 1 {
		t.Fatalf("hooks in GET = %v", def.Hooks)
	}
}

func TestOutboxEndpoints(t *testing.T) {
	s := newTestServerHooks(t, nil)
	c := login(s)
	seedDS(t, s)
	d := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
		Label: "T", KeyColumns: []string{"id"}, PageSize: 20}
	s.store.SaveTableDef(d, nil)
	s.store.EnqueueOutbox(&meta.OutboxEntry{TableDefID: d.ID, Event: "afterCreate",
		HookName: "H", NewValues: `{"a":1}`, Username: "admin"})
	s.store.MarkOutboxFailed(1, 6, "", "boom") // dead

	w := do(s, "GET", "/api/hooks/outbox?status=dead", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var res struct {
		Entries []struct{ ID, Status, HookName string }
		Total   int
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 1 || res.Entries[0].Status != "dead" || res.Entries[0].HookName != "H" {
		t.Fatalf("entries = %+v", res)
	}
	tok := s.ids.Encode("ob", 1)
	if w = do(s, "POST", "/api/hooks/outbox/"+tok+"/retry", "", c); w.Code != 200 {
		t.Fatalf("retry = %d %s", w.Code, w.Body)
	}
	if w = do(s, "POST", "/api/hooks/outbox/"+s.ids.Encode("ob", 99)+"/retry", "", c); w.Code != 404 {
		t.Fatalf("retry missing = %d", w.Code)
	}
}
