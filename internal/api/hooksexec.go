package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"ku-crud/internal/ds"
	"ku-crud/internal/hooks"
	"ku-crud/internal/meta"
)

func (s *Server) hookCtx(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef) *hooks.HookContext {
	return &hooks.HookContext{
		User:     hooks.ActingUser{ID: u.ID, Username: u.Username},
		TableDef: def, Columns: cols,
		Open:  func(id int64) (ds.Adapter, error) { return hooks.OpenDatasource(s.store, id) },
		Store: s.store, Logger: slog.Default(),
	}
}

// hookGuard rejects writes when an assignment references a hook absent
// from this binary (renamed/deleted hook, imported metadata).
func (s *Server) hookGuard(def *meta.TableDef) error {
	asgs, err := hooks.ParseAssignments(def.Hooks)
	if err != nil {
		return err
	}
	return s.hooks.CheckMissing(asgs)
}

// wrapHookErr maps a hook error onto a 400 apiError (HOOK_MISSING for a
// MissingError, HOOK_REJECTED otherwise) — for write paths without a
// ResponseWriter, e.g. m2m link sync.
func wrapHookErr(err error) error {
	var me *hooks.MissingError
	if errors.As(err, &me) {
		return newAPIErr(400, "HOOK_MISSING", err.Error())
	}
	return newAPIErr(400, "HOOK_REJECTED", err.Error())
}

func writeHookErr(w http.ResponseWriter, err error) {
	ae := wrapHookErr(err).(*apiError)
	writeErr(w, ae.status, ae.code, ae.msg, nil)
}

// runBefore executes the def's before-hooks for ev, returning the modified
// values map. All registry methods are nil-safe — no-hooks = passthrough.
func (s *Server) runBefore(ctx context.Context, u CtxUser, def *meta.TableDef,
	cols []meta.ColumnDef, ev hooks.Event, values, old map[string]any,
) (map[string]any, error) {
	asgs, err := hooks.ParseAssignments(def.Hooks)
	if err != nil {
		return values, err
	}
	if len(asgs[ev]) == 0 {
		return values, nil
	}
	if values == nil {
		values = map[string]any{}
	}
	out, err := s.hooks.RunBefore(ctx, ev, asgs[ev], s.hookCtx(u, def, cols),
		hooks.RowPayload{Values: values, Old: old})
	return out.Values, err
}

// enqueueAfter snapshots one outbox entry per after-hook assignment.
// Best-effort: the data write is already committed (audit-write policy).
func (s *Server) enqueueAfter(u CtxUser, def *meta.TableDef, ev hooks.Event,
	old, newV map[string]any,
) {
	asgs, err := hooks.ParseAssignments(def.Hooks)
	if err != nil || len(asgs[ev]) == 0 {
		return
	}
	var oldB, newB string
	if old != nil {
		oldB = string(mustJSON(old))
	}
	if newV != nil {
		newB = string(mustJSON(newV))
	}
	for _, a := range asgs[ev] {
		e := &meta.OutboxEntry{TableDefID: def.ID, Event: string(ev), HookName: a.Hook,
			Config: string(a.Config), OldValues: oldB, NewValues: newB,
			UserID: u.ID, Username: u.Username}
		if err := s.store.EnqueueOutbox(e); err != nil {
			slog.Warn("hook outbox enqueue failed", "def", def.ID,
				"event", ev, "hook", a.Hook, "err", err.Error())
		}
	}
}
