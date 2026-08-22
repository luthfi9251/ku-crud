package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

// backoff[N-1] is the wait after the Nth failed execution (N = 1..5).
// The 6th failure marks the entry dead: one initial execution + five
// retries (spec decision).
var backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, 4 * time.Hour}

const workerPoll = 5 * time.Second

// Worker drains the after-hook outbox. One worker per process; store access
// serializes with the API on the single SQLite connection.
type Worker struct {
	Store  *meta.Store
	Reg    *Registry
	Logger *slog.Logger
}

func (w *Worker) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(workerPoll)
	defer t.Stop()
	for {
		w.ExecuteDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ExecuteDue claims and runs the oldest due entries (one pass).
func (w *Worker) ExecuteDue(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339)
	due, err := w.Store.DueOutbox(now, 10)
	if err != nil {
		w.log().Error("hook outbox poll failed", "err", err.Error())
		return
	}
	for _, e := range due {
		w.execOne(ctx, e)
	}
}

func (w *Worker) execOne(ctx context.Context, e meta.OutboxEntry) {
	def, cols, err := w.Store.GetTableDef(e.TableDefID)
	if err != nil {
		// definition deleted (cascade normally removes entries; belt+braces)
		w.Store.MarkOutboxFailed(e.ID, 6, "", "table definition unavailable")
		return
	}
	hc := &HookContext{
		User:     ActingUser{ID: e.UserID, Username: e.Username},
		TableDef: def, Columns: cols,
		Open:   func(id int64) (ds.Adapter, error) { return OpenDatasource(w.Store, id) },
		Store:  w.Store,
		Logger: w.log(),
	}
	row := RowPayload{Values: map[string]any{}}
	if e.OldValues != "" {
		json.Unmarshal([]byte(e.OldValues), &row.Old)
	}
	if e.NewValues != "" {
		json.Unmarshal([]byte(e.NewValues), &row.Values)
	}
	err = w.Reg.RunOne(ctx, hc, Event(e.Event), row, Assignment{Hook: e.HookName, Config: json.RawMessage(e.Config)})
	if err == nil {
		w.Store.MarkOutboxDone(e.ID)
		return
	}
	var me *MissingError
	if errors.As(err, &me) { // can never succeed in this binary
		w.log().Error("hook outbox entry dead (hook missing)", "hook", e.HookName, "err", err.Error())
		w.Store.MarkOutboxFailed(e.ID, 6, "", err.Error())
		return
	}
	attempts := e.Attempts + 1
	if attempts >= len(backoff)+1 {
		w.log().Error("hook outbox entry dead (retry budget exhausted)", "hook", e.HookName, "attempts", attempts, "err", err.Error())
		w.Store.MarkOutboxFailed(e.ID, attempts, "", err.Error())
		return
	}
	next := time.Now().Add(backoff[attempts-1]).UTC().Format(time.RFC3339)
	w.log().Warn("hook execution failed, scheduled retry", "hook", e.HookName, "attempts", attempts, "retry_at", next, "err", err.Error())
	w.Store.MarkOutboxFailed(e.ID, attempts, next, err.Error())
}
