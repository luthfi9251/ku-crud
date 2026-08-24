// Package hooks defines the automation contract: developer-written Go
// functions assigned to CRUD events via table definitions. Code is trusted
// and compiled in; the registry is populated at build time by cmd/hookgen.
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

type Event string

const (
	BeforeCreate Event = "beforeCreate"
	AfterCreate  Event = "afterCreate"
	BeforeUpdate Event = "beforeUpdate"
	AfterUpdate  Event = "afterUpdate"
	BeforeDelete Event = "beforeDelete"
	AfterDelete  Event = "afterDelete"
	// OnAction is the custom row-action event (v1.9). It is deliberately
	// NOT part of allEvents: ParseAssignments rejects it — onAction hooks
	// are assigned via table_defs.actions, not the CRUD hooks map.
	OnAction Event = "onAction"
)

var allEvents = map[Event]bool{BeforeCreate: true, AfterCreate: true,
	BeforeUpdate: true, AfterUpdate: true, BeforeDelete: true, AfterDelete: true}

type ActingUser struct {
	ID       int64
	Username string
}

// RowPayload carries the write through hooks. Values is the new payload
// (empty map for delete); Old is the pre-write row (nil on create).
// Message is set by onAction hooks (custom row actions) and ignored by
// before/after hooks.
type RowPayload struct {
	Values  map[string]any
	Old     map[string]any
	Message string
}

// HookContext gives a hook full platform access: every registered
// datasource (lazily opened, hook must Close), the metadata store, a logger.
type HookContext struct {
	User     ActingUser
	TableDef *meta.TableDef
	Columns  []meta.ColumnDef
	Open     func(datasourceID int64) (ds.Adapter, error)
	Store    *meta.Store
	Logger   *slog.Logger
}

type HookFunc func(ctx context.Context, hc *HookContext, ev Event,
	row RowPayload, cfg json.RawMessage) (RowPayload, error)

// Registry maps hook names to functions. All methods are nil-safe: a nil
// registry behaves as "no hooks registered".
type Registry struct {
	mu    sync.RWMutex
	funcs map[string]HookFunc
}

var Default = NewRegistry()

// Register adds to the Default registry (used by generated code).
func Register(name string, fn HookFunc) error { return Default.Register(name, fn) }

func NewRegistry() *Registry { return &Registry{funcs: map[string]HookFunc{}} }

func (r *Registry) Register(name string, fn HookFunc) error {
	if r == nil {
		return errors.New("nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.funcs[name]; dup {
		return fmt.Errorf("hook %q registered twice", name)
	}
	r.funcs[name] = fn
	return nil
}

func (r *Registry) Get(name string) (HookFunc, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.funcs[name]
	return fn, ok
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.funcs))
	for n := range r.funcs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Assignment binds one registered hook to one event on one table
// definition, with per-assignment JSON config and execution order.
type Assignment struct {
	Hook   string          `json:"hook"`
	Config json.RawMessage `json:"config,omitempty"`
	Order  int             `json:"order"`
}

// Assignments is the parsed table_defs.hooks JSON.
type Assignments map[Event][]Assignment

func ParseAssignments(s string) (Assignments, error) {
	if len(s) == 0 {
		return Assignments{}, nil
	}
	var m map[string][]Assignment
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, errors.New("hooks is not valid JSON")
	}
	out := Assignments{}
	for k, list := range m {
		ev := Event(k)
		if !allEvents[ev] {
			return nil, fmt.Errorf("unknown hook event %q", k)
		}
		for _, a := range list {
			if a.Hook == "" {
				return nil, fmt.Errorf("event %q: hook name is required", k)
			}
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].Order < list[j].Order })
		out[ev] = list
	}
	return out, nil
}

// Names returns the unique hook names across all events.
func (a Assignments) Names() []string {
	seen := map[string]bool{}
	for _, list := range a {
		for _, asg := range list {
			seen[asg.Hook] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// hideKeys are the built-in grid actions that can be hidden per table.
var hideKeys = map[string]bool{
	"newRow": true, "edit": true, "delete": true, "copy": true,
	"import": true, "export": true, "refresh": true,
}

var actionGrants = map[string]bool{"read": true, "create": true, "update": true, "delete": true}
var actionStyles = map[string]bool{"neutral": true, "primary": true, "danger": true}

var actionIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// CustomAction is one programmer-defined row action button (v1.9).
type CustomAction struct {
	ID      string          `json:"id"`
	Label   string          `json:"label"`
	Confirm string          `json:"confirm,omitempty"`
	Grant   string          `json:"grant"` // read|create|update|delete
	Hook    string          `json:"hook"`
	Config  json.RawMessage `json:"config,omitempty"`
	Order   int             `json:"order"`
	Style   string          `json:"style"` // neutral|primary|danger
}

// ActionsConfig is the parsed table_defs.actions JSON.
type ActionsConfig struct {
	Hidden []string       `json:"hidden,omitempty"`
	Custom []CustomAction `json:"custom,omitempty"`
}

// Find returns the custom action with the given id, or nil.
func (c ActionsConfig) Find(id string) *CustomAction {
	for i := range c.Custom {
		if c.Custom[i].ID == id {
			return &c.Custom[i]
		}
	}
	return nil
}

// ParseActions validates the table_defs.actions JSON: hidden keys from the
// fixed set, custom entries with unique slug ids, non-empty label/hook,
// valid grant/style. Custom is returned sorted by Order.
func ParseActions(s string) (ActionsConfig, error) {
	out := ActionsConfig{}
	if len(s) == 0 {
		return out, nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return out, errors.New("actions is not valid JSON")
	}
	seen := map[string]bool{}
	for _, h := range out.Hidden {
		if !hideKeys[h] {
			return out, fmt.Errorf("unknown hidden action %q", h)
		}
	}
	for i := range out.Custom {
		a := &out.Custom[i]
		if !actionIDRe.MatchString(a.ID) {
			return out, fmt.Errorf("action %d: id must be 1-64 chars of [a-zA-Z0-9_-]", i+1)
		}
		if seen[a.ID] {
			return out, fmt.Errorf("duplicate action id %q", a.ID)
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Label) == "" {
			return out, fmt.Errorf("action %q: label is required", a.ID)
		}
		if a.Hook == "" {
			return out, fmt.Errorf("action %q: hook is required", a.ID)
		}
		if !actionGrants[a.Grant] {
			return out, fmt.Errorf("action %q: grant must be read, create, update or delete", a.ID)
		}
		if a.Style == "" {
			a.Style = "neutral"
		} else if !actionStyles[a.Style] {
			return out, fmt.Errorf("action %q: style must be neutral, primary or danger", a.ID)
		}
	}
	sort.SliceStable(out.Custom, func(i, j int) bool { return out.Custom[i].Order < out.Custom[j].Order })
	return out, nil
}
