package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/luthfi9251/kucrud-core/engine"
	"github.com/luthfi9251/kucrud-core/hooks"
)

// hookEnv wires a server with three hooks: RejectNames refuses values of
// "name" equal to "forbidden"; UpperName uppercases "name"; NoteAfter is a
// counting after-hook. seedLive (rows_test.go) creates def 1 on live PG.
func hookEnv(t *testing.T) (*Server, *int) {
	t.Helper()
	reg := hooks.NewRegistry()
	reg.Register("RejectNames", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		if row.Values["name"] == "forbidden" || row.Old["name"] == "forbidden" {
			return row, errors.New("name is forbidden")
		}
		return row, nil
	})
	afterRuns := 0
	reg.Register("NoteAfter", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		afterRuns++
		return row, nil
	})
	reg.Register("UpperName", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		if n, ok := row.Values["name"].(string); ok {
			row.Values["name"] = strings.ToUpper(n)
		}
		return row, nil
	})
	s := newTestServerHooks(t, reg)
	seedLive(t, s) // creates def 1 on the live PG (columns incl. name; makes ds ids deterministic)
	return s, &afterRuns
}

func TestBeforeCreateRejectAndMutate(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s, _ := hookEnv(t)
	c := login(s)
	setHooks(t, s, 1, `{"beforeCreate":[{"hook":"UpperName","order":1}],"afterCreate":[{"hook":"NoteAfter","order":1}]}`)

	tok := tdTok(s, 1)
	w := do(s, "POST", "/api/tables/"+tok+"/rows", `{"name":"nia","status":"sunny"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	// mutation landed (uppercase) — verify via list search
	w = do(s, "GET", "/api/tables/"+tok+"/rows?search=NIA", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "NIA") {
		t.Fatalf("mutated row not found: %d %s", w.Code, w.Body)
	}
}

func TestBeforeCreateRejection(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s, _ := hookEnv(t)
	c := login(s)
	setHooks(t, s, 1, `{"beforeCreate":[{"hook":"RejectNames","order":1}]}`)
	w := do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"name":"forbidden"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_REJECTED") {
		t.Fatalf("reject = %d %s", w.Code, w.Body)
	}
	// nothing inserted
	w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows?search=forbidden", "", c)
	if strings.Contains(w.Body.String(), "forbidden") {
		t.Fatal("rejected row must not exist")
	}
}

func TestAfterEnqueued(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s, _ := hookEnv(t)
	c := login(s)
	setHooks(t, s, 1, `{"afterCreate":[{"hook":"NoteAfter","order":1}]}`)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"name":"enia"}`, c)
	entries, total, _ := s.store.ListOutbox("", 0, 10, 0)
	if total != 1 || entries[0].HookName != "NoteAfter" || entries[0].Event != "afterCreate" {
		t.Fatalf("outbox = %+v", entries)
	}
	if entries[0].NewValues == "" || entries[0].Username == "" {
		t.Fatalf("entry snapshot = %+v", entries[0])
	}
}

func TestHookMissingBlocksWrite(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServerHooks(t, hooks.NewRegistry()) // empty registry
	c := login(s)
	seedLive(t, s)
	setHooks(t, s, 1, `{"beforeCreate":[{"hook":"Ghost","order":1}]}`)
	w := do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"name":"x"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_MISSING") {
		t.Fatalf("missing = %d %s", w.Code, w.Body)
	}
}

func TestBeforeDeleteRejection(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s, _ := hookEnv(t)
	c := login(s)
	// create with an explicit key so the row key is known (PKs are insertable)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":50,"name":"forbidden"}`, c)
	setHooks(t, s, 1, `{"beforeDelete":[{"hook":"RejectNames","order":1}]}`)
	w := do(s, "DELETE", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"50"}), "", c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_REJECTED") {
		t.Fatalf("delete reject = %d %s", w.Code, w.Body)
	}
	// row survives
	w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows?search=forbidden", "", c)
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Fatal("row must survive a rejected delete")
	}
}

func TestAfterDeleteEnqueues(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s, _ := hookEnv(t)
	c := login(s)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":51,"name":"gone"}`, c)
	setHooks(t, s, 1, `{"afterDelete":[{"hook":"NoteAfter","order":1}]}`)
	w := do(s, "DELETE", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"51"}), "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	entries, total, _ := s.store.ListOutbox("", 0, 10, 0)
	if total != 1 || entries[0].Event != "afterDelete" || entries[0].HookName != "NoteAfter" {
		t.Fatalf("outbox = %+v", entries)
	}
	if !strings.Contains(entries[0].OldValues, "gone") {
		t.Fatalf("old values snapshot = %q", entries[0].OldValues)
	}
}

func TestBeforeUpdateRejectAndAfterEnqueue(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s, _ := hookEnv(t)
	c := login(s)
	do(s, "POST", "/api/tables/"+tdTok(s, 1)+"/rows", `{"id":60,"name":"mia"}`, c)

	// beforeUpdate rejection: row keeps its old name
	setHooks(t, s, 1, `{"beforeUpdate":[{"hook":"RejectNames","order":1}]}`)
	w := do(s, "PUT", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"60"}), `{"name":"forbidden"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_REJECTED") {
		t.Fatalf("update reject = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows?search=mia", "", c); !strings.Contains(w.Body.String(), "mia") {
		t.Fatal("row must keep old name after rejected update")
	}

	// afterUpdate enqueue: valid PUT snapshots old + merged new values
	setHooks(t, s, 1, `{"afterUpdate":[{"hook":"NoteAfter","order":1}]}`)
	w = do(s, "PUT", "/api/tables/"+tdTok(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"60"}), `{"name":"mia2"}`, c)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	entries, total, _ := s.store.ListOutbox("", 0, 10, 0)
	if total != 1 || entries[0].Event != "afterUpdate" || entries[0].HookName != "NoteAfter" {
		t.Fatalf("outbox = %d %+v", total, entries)
	}
	if !strings.Contains(entries[0].OldValues, "mia") || !strings.Contains(entries[0].NewValues, "mia2") {
		t.Fatalf("outbox snapshot old=%s new=%s", entries[0].OldValues, entries[0].NewValues)
	}
}

// setHooks writes hooks JSON onto a stored def directly (bypasses the API's
// save-time registry check so tests can simulate drift).
func setHooks(t *testing.T, s *Server, defID int64, hooksJSON string) {
	t.Helper()
	def, cols, err := s.store.GetTableDef(defID)
	if err != nil {
		t.Fatal(err)
	}
	def.Hooks = hooksJSON
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
}
