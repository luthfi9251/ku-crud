package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/luthfi9251/ku-crud/core/defs"
	"github.com/luthfi9251/ku-crud/core/ds"
	"github.com/luthfi9251/ku-crud/core/hooks"
	kuhooks "ku-crud/internal/hooks"
	"ku-crud/internal/meta"
)

// hookCtx builds the definitions-shaped hook context for one request:
// the actor from the request user, the table as the ID-free core
// contract, a name-keyed datasource opener backed by the meta resolver
// (name → definition → its datasource), and the store as Host.
func (s *Server) hookCtx(actor string, res *metaResolver, t *defs.Table) *hooks.HookContext {
	return &hooks.HookContext{
		Actor:   actor,
		Table:   t,
		Columns: t.Columns,
		Open: func(name string) (ds.Adapter, error) {
			d, ok := res.byName[name]
			if !ok || name == "" {
				return nil, fmt.Errorf("unknown table definition %q", name)
			}
			return kuhooks.OpenDatasource(s.store, d.DatasourceID)
		},
		Host:   s.store,
		Logger: slog.Default(),
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

// apiError routes an exact http status/code/message through helper flows
// that don't own a ResponseWriter (wrapHookErr and friends).
type apiError struct {
	status int
	code   string
	msg    string
}

func (e *apiError) Error() string { return e.msg }
func newAPIErr(status int, code, msg string) *apiError {
	return &apiError{status: status, code: code, msg: msg}
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
// Assignments come from the core table (the same JSON the stored def
// carries); hc carries actor/table/opener.
func (s *Server) runBefore(ctx context.Context, hc *hooks.HookContext,
	t *defs.Table, ev hooks.Event, values, old map[string]any,
) (map[string]any, error) {
	asgs, err := hooks.ParseAssignments(t.Hooks)
	if err != nil {
		return values, err
	}
	if len(asgs[ev]) == 0 {
		return values, nil
	}
	if values == nil {
		values = map[string]any{}
	}
	out, err := s.hooks.RunBefore(ctx, ev, asgs[ev], hc,
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
