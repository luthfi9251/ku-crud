package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

const (
	// BeforeTimeout bounds a synchronous before-hook inside the request.
	BeforeTimeout = 5 * time.Second
	// AfterTimeout bounds an after-hook on the worker.
	AfterTimeout = 30 * time.Second
	// ActionTimeout bounds a synchronous custom-action hook inside the
	// request (longer than before-hooks: actions are interactive intent).
	ActionTimeout = 15 * time.Second
)

// MissingError marks an assignment referencing a hook absent from the
// binary (deleted/renamed hook, or metadata imported from elsewhere).
type MissingError struct{ Name string }

func (e *MissingError) Error() string {
	return "hook " + e.Name + " is not registered in this binary"
}

// CheckMissing verifies every referenced hook name exists in the registry.
func (r *Registry) CheckMissing(asgs Assignments) error {
	for _, name := range asgs.Names() {
		if _, ok := r.Get(name); !ok {
			return &MissingError{Name: name}
		}
	}
	return nil
}

// runGuarded executes one hook with timeout and panic recovery; a broken
// hook becomes an error, never a crash.
func (r *Registry) runGuarded(parent context.Context, timeout time.Duration,
	fn HookFunc, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage,
) (out RowPayload, err error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			out, err = RowPayload{}, fmt.Errorf("hook panicked: %v", p)
		}
	}()
	return fn(ctx, hc, ev, row, cfg)
}

// RunBefore executes ordered before-hooks synchronously, threading the
// (possibly modified) payload through each. A nil registry is a no-op.
func (r *Registry) RunBefore(ctx context.Context, ev Event, asgs []Assignment,
	hc *HookContext, row RowPayload,
) (RowPayload, error) {
	if r == nil {
		return row, nil
	}
	for _, a := range asgs {
		fn, ok := r.Get(a.Hook)
		if !ok {
			return row, &MissingError{Name: a.Hook}
		}
		var err error
		row, err = r.runGuarded(ctx, BeforeTimeout, fn, hc, ev, row, a.Config)
		if err != nil {
			return row, err
		}
	}
	return row, nil
}

// RunOne executes a single assignment (worker path for after-hooks).
func (r *Registry) RunOne(ctx context.Context, hc *HookContext, ev Event,
	row RowPayload, a Assignment,
) error {
	if r == nil {
		return nil
	}
	fn, ok := r.Get(a.Hook)
	if !ok {
		return &MissingError{Name: a.Hook}
	}
	_, err := r.runGuarded(ctx, AfterTimeout, fn, hc, ev, row, a.Config)
	return err
}

// OpenDatasource opens a live adapter for a stored datasource id — the
// opener injected into HookContext by both the API and the worker.
func OpenDatasource(store *meta.Store, dsID int64) (ds.Adapter, error) {
	d, err := store.GetDatasource(dsID)
	if err != nil {
		return nil, err
	}
	return ds.Open(ds.Conn{Driver: d.Driver, Host: d.Host, Port: d.Port,
		DB: d.DBName, User: d.Username, Password: d.Password, SSLMode: d.SSLMode, Raw: d.Raw})
}

// RunAction executes one custom action's hook synchronously and returns
// the hook's result message (RowPayload.Message). Modified values are NOT
// written back — custom actions are side effect + message only; the hook
// itself has full platform access when it wants to persist changes.
func (r *Registry) RunAction(ctx context.Context, hc *HookContext,
	row RowPayload, a Assignment,
) (string, error) {
	if r == nil {
		return "", &MissingError{Name: a.Hook}
	}
	fn, ok := r.Get(a.Hook)
	if !ok {
		return "", &MissingError{Name: a.Hook}
	}
	out, err := r.runGuarded(ctx, ActionTimeout, fn, hc, OnAction, row, a.Config)
	if err != nil {
		return "", err
	}
	return out.Message, nil
}
