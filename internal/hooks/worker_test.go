package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	corehooks "github.com/luthfi9251/ku-crud/core/hooks"

	"ku-crud/internal/meta"
)

func newWorkerStore(t *testing.T) *meta.Store {
	t.Helper()
	s, err := meta.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.CreateDatasource(&meta.Datasource{Name: "d", Host: "h", Port: 1, DBName: "db", Username: "u", Password: "p"})
	return s
}

func TestWorkerExecutesAndRetries(t *testing.T) {
	store := newWorkerStore(t)
	def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
		Label: "T", KeyColumns: []string{"id"}, PageSize: 20,
		Hooks: `{"afterCreate":[{"hook":"H","order":1}]}`}
	store.SaveTableDef(def, nil)

	var runs int
	reg := corehooks.NewRegistry()
	reg.Register("H", func(ctx context.Context, hc *corehooks.HookContext, ev corehooks.Event, row corehooks.RowPayload, cfg json.RawMessage) (corehooks.RowPayload, error) {
		runs++
		if hc.Actor != "admin" || hc.Table.Name != def.TableName || hc.Host != store {
			t.Errorf("hook ctx = %+v", hc)
		}
		if row.Values["x"] != float64(1) {
			t.Errorf("payload = %+v", row.Values)
		}
		if runs == 1 {
			return row, errors.New("transient")
		}
		return row, nil
	})
	w := &Worker{Store: store, Reg: reg, Logger: slog.Default()}

	store.EnqueueOutbox(&meta.OutboxEntry{TableDefID: def.ID, Event: "afterCreate",
		HookName: "H", NewValues: `{"x":1}`, UserID: 1, Username: "admin"})

	w.ExecuteDue(context.Background()) // first run fails
	e, _ := store.GetOutbox(1)
	if e.Status != "pending" || e.Attempts != 1 || e.LastError != "transient" {
		t.Fatalf("after 1st failure = %+v", e)
	}
	if due, _ := store.DueOutbox(time.Now().UTC().Format(time.RFC3339), 10); len(due) != 0 {
		t.Fatal("entry must not be due before next_retry_at")
	}

	// force it due again, second run succeeds
	store.MarkOutboxFailed(1, 1, time.Now().UTC().Format(time.RFC3339), "transient") // simulate due time = now
	// (MarkOutboxFailed with a now timestamp makes it immediately due)
	w.ExecuteDue(context.Background())
	e, _ = store.GetOutbox(1)
	if e.Status != "done" || runs != 2 {
		t.Fatalf("after 2nd run = %+v runs=%d", e, runs)
	}
}

func TestWorkerDeadAfterSixFailures(t *testing.T) {
	store := newWorkerStore(t)
	def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
		Label: "T", KeyColumns: []string{"id"}, PageSize: 20}
	store.SaveTableDef(def, nil)
	reg := corehooks.NewRegistry()
	reg.Register("Bad", func(ctx context.Context, hc *corehooks.HookContext, ev corehooks.Event, row corehooks.RowPayload, cfg json.RawMessage) (corehooks.RowPayload, error) {
		return row, errors.New("always")
	})
	w := &Worker{Store: store, Reg: reg, Logger: slog.Default()}
	store.EnqueueOutbox(&meta.OutboxEntry{TableDefID: def.ID, Event: "afterDelete", HookName: "Bad"})

	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 6; i++ {
		w.ExecuteDue(context.Background())
		e, _ := store.GetOutbox(1)
		if e.Status == "dead" {
			break
		}
		if e.Attempts != i+1 {
			t.Fatalf("attempts = %d", e.Attempts)
		}
		// make due immediately for the next pass (worker normally waits backoff)
		store.MarkOutboxFailed(1, e.Attempts, now, e.LastError)
	}
	e, _ := store.GetOutbox(1)
	if e.Status != "dead" {
		t.Fatalf("expected dead after 6, got %+v", e)
	}
}

func TestWorkerMissingHookGoesDead(t *testing.T) {
	store := newWorkerStore(t)
	def := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
		Label: "T", KeyColumns: []string{"id"}, PageSize: 20}
	store.SaveTableDef(def, nil)
	w := &Worker{Store: store, Reg: corehooks.NewRegistry(), Logger: slog.Default()}
	store.EnqueueOutbox(&meta.OutboxEntry{TableDefID: def.ID, Event: "afterCreate", HookName: "Gone"})
	w.ExecuteDue(context.Background())
	e, _ := store.GetOutbox(1)
	if e.Status != "dead" {
		t.Fatalf("missing hook should die immediately: %+v", e)
	}
}
