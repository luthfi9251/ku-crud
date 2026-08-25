package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"ku-crud/internal/defs"
)

// Event mirrors the platform's hook event names. The engine keeps a local
// copy because the hooks package still depends on platform metadata; the
// two unify when hooks moves into core.
type Event string

const (
	BeforeCreate Event = "beforeCreate"
	AfterCreate  Event = "afterCreate"
	BeforeUpdate Event = "beforeUpdate"
	AfterUpdate  Event = "afterUpdate"
	BeforeDelete Event = "beforeDelete"
	AfterDelete  Event = "afterDelete"
)

// RowPayload carries the write through hooks: Values is the new payload
// (nil on delete), Old the pre-write row (nil on create).
type RowPayload struct {
	Values map[string]any
	Old    map[string]any
}

// HookError carries hook failure semantics across the boundary (the
// engine cannot import the hooks package, which still depends on meta).
// Missing marks an assignment naming a hook absent from the registry.
type HookError struct {
	Missing bool
	Msg     string
}

func (e *HookError) Error() string { return e.Msg }

// Hooks is the zero-persistence callback contract. Guard rejects a write
// when a definition's assignments are unusable; RunBefore runs
// synchronously before the write and may rewrite the payload; RunAfter
// schedules post-commit side effects and must not fail the request (the
// platform's outbox is one possible implementation).
type Hooks interface {
	Guard(t *defs.Table) error
	RunBefore(ev Event, t *defs.Table, row RowPayload) (RowPayload, error)
	RunAfter(ev Event, t *defs.Table, row RowPayload) error
}

// RefSource is one inbound fk reference for delete protection: Src is the
// referencing definition, Column its fk column, RefColumn the referenced
// column on the target table, Label the conflict-detail display label.
type RefSource struct {
	Src       *defs.Table
	Column    string
	RefColumn string
	Label     string
}

// WriteService renders the write endpoints (insert/update/delete/bulk
// delete). It is auth-free like ReadService: the platform checks
// table-level grants before calling; junction grants and inbound-fk
// discovery ride on the callbacks. Audit rows are not written here —
// audit returns as an AfterX hook (platformhooks).
type WriteService struct {
	R Resolver
	H Hooks // may be nil
	// CanWrite reports the acting user's grant on related definitions
	// (junction tables of m2m columns). Nil allows everything.
	CanWrite func(table, grant string) bool
	// RefSources lists definitions whose fk columns reference t (delete
	// protection); the platform derives it from metadata.
	RefSources func(t *defs.Table) ([]RefSource, error)
}

// statusErr routes an exact http error through helper flows (m2m sync).
type statusErr struct {
	status int
	code   string
	msg    string
}

func (e *statusErr) Error() string { return e.msg }

func writeHookErr(w http.ResponseWriter, err error) {
	var he *HookError
	if errors.As(err, &he) && he.Missing {
		writeErr(w, 400, "HOOK_MISSING", he.Msg, nil)
		return
	}
	writeErr(w, 400, "HOOK_REJECTED", err.Error(), nil)
}

// hookStatusErr maps a hook failure onto the 400 m2m-sync error shape.
func hookStatusErr(err error) error {
	var he *HookError
	if errors.As(err, &he) && he.Missing {
		return &statusErr{status: 400, code: "HOOK_MISSING", msg: he.Msg}
	}
	return &statusErr{status: 400, code: "HOOK_REJECTED", msg: err.Error()}
}

func writeM2MErr(w http.ResponseWriter, err error) {
	var se *statusErr
	if errors.As(err, &se) {
		writeErr(w, se.status, se.code, se.msg, nil)
		return
	}
	writeErr(w, 502, "CONN", "relation sync failed", err.Error())
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

func (s *WriteService) canWrite(table, grant string) bool {
	return s.CanWrite == nil || s.CanWrite(table, grant)
}

func (s *WriteService) guard(t *defs.Table) error {
	if s.H == nil {
		return nil
	}
	return s.H.Guard(t)
}

func (s *WriteService) runBefore(ev Event, t *defs.Table, row RowPayload) (RowPayload, error) {
	if s.H == nil {
		return row, nil
	}
	return s.H.RunBefore(ev, t, row)
}

func (s *WriteService) runAfter(ev Event, t *defs.Table, row RowPayload) {
	if s.H == nil {
		return
	}
	if err := s.H.RunAfter(ev, t, row); err != nil {
		slog.Warn("after-hook scheduling failed", "event", ev, "table", t.Name, "err", err.Error())
	}
}

// errFKRefNotFound marks an fk payload value that has no matching target row.
var errFKRefNotFound = errors.New("referenced row not found")

// m2mSelection extracts []any target ref values from a request body value.
func m2mSelection(v any) ([]any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case []any:
		return t, nil
	default:
		return nil, fmt.Errorf("many-to-many selection must be an array of target values")
	}
}

// stripM2MPayload removes m2m selections from the body (they must never
// reach EditablePayload) and returns them per column.
func stripM2MPayload(cols []defs.Column, body map[string]any) (map[string][]any, error) {
	var out map[string][]any
	for _, c := range cols {
		if c.FieldType != "m2m" {
			continue
		}
		v, present := body[c.Name]
		if !present {
			continue
		}
		delete(body, c.Name)
		want, err := m2mSelection(v)
		if err != nil {
			return nil, &statusErr{status: 400, code: "VALIDATION",
				msg: "column " + c.Name + ": " + err.Error()}
		}
		if out == nil {
			out = map[string][]any{}
		}
		out[c.Name] = want
	}
	return out, nil
}

// precheckM2M validates configs and junction grants before any parent
// write, so permission failures reject the whole request atomically.
// (Junction/target resolution is the shared ResolveM2M from rels.go.)
func (s *WriteService) precheckM2M(t *defs.Table, selections map[string][]any) error {
	for _, c := range t.Columns {
		if _, ok := selections[c.Name]; !ok {
			continue
		}
		cfg, msg := ResolveM2M(s.R, t, c)
		if cfg == nil {
			return &statusErr{status: 400, code: "VALIDATION", msg: msg}
		}
		if !s.canWrite(cfg.Junction.Name, "create") || !s.canWrite(cfg.Junction.Name, "delete") {
			return &statusErr{status: 403, code: "FORBIDDEN",
				msg: "managing " + cfg.Junction.Label + " relations requires create and delete grants on that table"}
		}
	}
	return nil
}

// applyM2MPayload syncs stripped selections after the row write committed.
// srcRow (old row, update path) supplies the source ref value; on insert
// it comes from the payload via the resolved config.
func (s *WriteService) applyM2MPayload(t *defs.Table, body map[string]any,
	srcRow map[string]any, selections map[string][]any,
) error {
	for _, c := range t.Columns {
		want, ok := selections[c.Name]
		if !ok {
			continue
		}
		cfg, msg := ResolveM2M(s.R, t, c)
		if cfg == nil {
			return &statusErr{status: 400, code: "VALIDATION", msg: msg}
		}
		var srcVal any
		if srcRow != nil {
			srcVal = srcRow[cfg.SrcRef]
		}
		if srcVal == nil {
			srcVal = body[cfg.SrcRef]
		}
		if srcVal == nil {
			return &statusErr{status: 400, code: "VALIDATION",
				msg: "column " + c.Name + ": provide the " + cfg.SrcRef + " value so relations can be created"}
		}
		if err := s.syncM2MLinks(c, cfg, srcVal, want); err != nil {
			return err
		}
	}
	return nil
}

// syncM2MLinks diffs the wanted target values against the current junction
// rows for one source value and applies inserts/deletes. Requires create
// and delete grants on the junction definition.
func (s *WriteService) syncM2MLinks(c defs.Column, cfg *M2MCfg, srcVal any, want []any) error {
	if srcVal == nil {
		return &statusErr{status: 400, code: "VALIDATION",
			msg: "column " + c.Name + ": cannot manage relations without a source key value"}
	}
	if !s.canWrite(cfg.Junction.Name, "create") || !s.canWrite(cfg.Junction.Name, "delete") {
		return &statusErr{status: 403, code: "FORBIDDEN",
			msg: "managing " + cfg.Junction.Label + " relations requires create and delete grants on that table"}
	}
	if err := s.guard(cfg.Junction); err != nil {
		return hookStatusErr(err)
	}
	ja, err := s.R.Adapter(cfg.Junction)
	if err != nil {
		return err
	}
	defer ja.Close()
	pairs, err := ja.FetchPairsByRef(cfg.Junction.Schema, cfg.Junction.PhysTab,
		c.M2M.SrcCol, c.M2M.TgtCol, []any{srcVal})
	if err != nil {
		return err
	}
	current := map[string]any{}
	for _, p := range pairs {
		if p.Ret != nil {
			current[rowValKey(p.Ret)] = p.Ret
		}
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[rowValKey(w)] = true
	}
	for _, w := range want { // added links
		if _, exists := current[rowValKey(w)]; exists {
			continue
		}
		linkVals := map[string]any{c.M2M.SrcCol: srcVal, c.M2M.TgtCol: w}
		if _, err := s.runBefore(BeforeCreate, cfg.Junction, RowPayload{Values: linkVals}); err != nil {
			return hookStatusErr(err)
		}
		if err := ja.Insert(cfg.Junction.Schema, cfg.Junction.PhysTab,
			[]string{c.M2M.SrcCol, c.M2M.TgtCol}, []any{srcVal, w}); err != nil {
			return err
		}
		// audit returns in Task 11 (platformhooks)
		s.runAfter(AfterCreate, cfg.Junction, RowPayload{Values: linkVals})
	}
	for k, v := range current { // removed links
		if wantSet[k] {
			continue
		}
		linkVals := map[string]any{c.M2M.SrcCol: srcVal, c.M2M.TgtCol: v}
		if _, err := s.runBefore(BeforeDelete, cfg.Junction, RowPayload{Old: linkVals}); err != nil {
			return hookStatusErr(err)
		}
		if _, err := ja.DeletePairs(cfg.Junction.Schema, cfg.Junction.PhysTab,
			c.M2M.SrcCol, srcVal, c.M2M.TgtCol, v); err != nil {
			return err
		}
		// audit returns in Task 11 (platformhooks)
		s.runAfter(AfterDelete, cfg.Junction, RowPayload{Old: linkVals})
	}
	return nil
}

// checkFKValues verifies each non-null fk payload value exists on the
// target table (batched per fk column via the ref-value IN query).
func (s *WriteService) checkFKValues(t *defs.Table, names []string, vals []any) error {
	for i, name := range names {
		var c *defs.Column
		for j := range t.Columns {
			if t.Columns[j].Name == name && t.Columns[j].FieldType == "fk" {
				c = &t.Columns[j]
				break
			}
		}
		if c == nil || vals[i] == nil {
			continue
		}
		if c.FK == nil {
			return fmt.Errorf("%s: fk target unavailable", name)
		}
		if c.FK.Table == defs.MissingTable {
			// drifted target def — the old flow's def lookup failed the
			// same way and surfaced as 502 "reference check failed"
			return fmt.Errorf("%s: fk target unavailable", name)
		}
		target := t
		if c.FK.Table != "" {
			resolved, err := s.R.Resolve(c.FK.Table)
			if err != nil {
				return fmt.Errorf("%s: fk target unavailable", name)
			}
			target = resolved
		}
		a, err := s.R.Adapter(target)
		if err != nil {
			return err
		}
		m, err := a.FetchByRefValues(target.Schema, target.PhysTab, c.FK.RefColumn, nil, []any{vals[i]})
		a.Close()
		if err != nil {
			return err
		}
		if len(m) == 0 {
			return fmt.Errorf("%s: %w", name, errFKRefNotFound)
		}
	}
	return nil
}

// referencedBy reports defined tables whose fk columns point at t and hold
// rows referencing the given old row. Detail rows feed the 409 body.
func (s *WriteService) referencedBy(t *defs.Table, old map[string]any) ([]map[string]any, error) {
	if s.RefSources == nil {
		return nil, nil
	}
	srcs, err := s.RefSources(t)
	if err != nil {
		return nil, err
	}
	var conflicts []map[string]any
	for _, src := range srcs {
		refVal := old[src.RefColumn]
		if refVal == nil {
			continue
		}
		a, err := s.R.Adapter(src.Src)
		if err != nil {
			return nil, err
		}
		n, err := a.CountByRefEq(src.Src.Schema, src.Src.PhysTab, src.Column, refVal)
		a.Close()
		if err != nil {
			return nil, err
		}
		if n > 0 {
			conflicts = append(conflicts, map[string]any{"table": src.Label, "column": src.Column, "count": n})
		}
	}
	return conflicts, nil
}

// mergeConflicts unions two delete-protection conflict lists, summing count
// for entries sharing the same table+column pair.
func mergeConflicts(existing []map[string]any, add []map[string]any) []map[string]any {
	for _, c := range add {
		merged := false
		for _, e := range existing {
			if e["table"] == c["table"] && e["column"] == c["column"] {
				e["count"] = e["count"].(int) + c["count"].(int)
				merged = true
				break
			}
		}
		if !merged {
			existing = append(existing, c)
		}
	}
	return existing
}

// Insert renders POST rows: validate, run before-hooks, insert, sync m2m
// selections, schedule after-hooks.
func (s *WriteService) Insert(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	selections, err := stripM2MPayload(t.Columns, body)
	if err != nil {
		var se *statusErr
		if errors.As(err, &se) {
			writeErr(w, se.status, se.code, se.msg, nil)
			return
		}
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if err := s.precheckM2M(t, selections); err != nil {
		var se *statusErr
		if errors.As(err, &se) {
			writeErr(w, se.status, se.code, se.msg, nil)
			return
		}
		writeErr(w, 500, "INTERNAL", "relation check failed", nil)
		return
	}
	names, vals, err := EditablePayload(t, body, true)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if err := s.guard(t); err != nil {
		writeHookErr(w, err)
		return
	}
	out, err := s.runBefore(BeforeCreate, t, RowPayload{Values: body})
	if err != nil {
		writeHookErr(w, err)
		return
	}
	body = out.Values
	names, vals, err = EditablePayload(t, body, true)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()
	if err := s.checkFKValues(t, names, vals); err != nil {
		if errors.Is(err, errFKRefNotFound) {
			writeErr(w, 400, "VALIDATION", err.Error(), nil)
			return
		}
		writeErr(w, 502, "CONN", "reference check failed", err.Error())
		return
	}
	if err := a.Insert(t.Schema, t.PhysTab, names, vals); err != nil {
		if a.IsFKViolation(err) {
			writeErr(w, 409, "CONFLICT", "row is referenced by other rows (database constraint)", nil)
			return
		}
		writeErr(w, 502, "CONN", "insert failed", err.Error())
		return
	}
	if err := s.applyM2MPayload(t, body, nil, selections); err != nil {
		writeM2MErr(w, err)
		return
	}
	// audit returns in Task 11 (platformhooks)
	s.runAfter(AfterCreate, t, RowPayload{Values: body})
	writeJSON(w, 200, map[string]any{"ok": true})
}

// Update renders PUT row: fetch old, run before-hooks, update, schedule
// after-hooks with old+merged, sync m2m selections.
func (s *WriteService) Update(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	selections, err := stripM2MPayload(t.Columns, body)
	if err != nil {
		var se *statusErr
		if errors.As(err, &se) {
			writeErr(w, se.status, se.code, se.msg, nil)
			return
		}
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if err := s.precheckM2M(t, selections); err != nil {
		var se *statusErr
		if errors.As(err, &se) {
			writeErr(w, se.status, se.code, se.msg, nil)
			return
		}
		writeErr(w, 500, "INTERNAL", "relation check failed", nil)
		return
	}
	names, vals, err := EditablePayload(t, body, false)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	pkVals, err := DecodeKey(t, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	oldRows, err := a.FetchByKey(t.Schema, t.PhysTab, t.Keys, pkVals, realColNames(t.Columns))
	if err != nil {
		writeErr(w, 502, "CONN", "fetch old failed", err.Error())
		return
	}
	if len(oldRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}

	if err := s.checkFKValues(t, names, vals); err != nil {
		if errors.Is(err, errFKRefNotFound) {
			writeErr(w, 400, "VALIDATION", err.Error(), nil)
			return
		}
		writeErr(w, 502, "CONN", "reference check failed", err.Error())
		return
	}

	if err := s.guard(t); err != nil {
		writeHookErr(w, err)
		return
	}
	out, err := s.runBefore(BeforeUpdate, t, RowPayload{Values: body, Old: oldRows[0]})
	if err != nil {
		writeHookErr(w, err)
		return
	}
	body = out.Values
	names, vals, err = EditablePayload(t, body, false)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}

	if _, err := a.UpdateByKey(t.Schema, t.PhysTab, names, vals, t.Keys, pkVals); err != nil {
		if a.IsFKViolation(err) {
			writeErr(w, 409, "CONFLICT", "row is referenced by other rows (database constraint)", nil)
			return
		}
		writeErr(w, 502, "CONN", "update failed", err.Error())
		return
	}
	// ponytail: per-row new = old merged with payload (computed cols not re-read)
	var mergedLast map[string]any
	for _, old := range oldRows {
		merged := map[string]any{}
		for k, v := range old {
			merged[k] = v
		}
		for k, v := range body {
			merged[k] = v
		}
		mergedLast = merged
		// audit returns in Task 11 (platformhooks)
	}
	s.runAfter(AfterUpdate, t, RowPayload{Values: mergedLast, Old: oldRows[0]})
	if err := s.applyM2MPayload(t, body, oldRows[0], selections); err != nil {
		writeM2MErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "affected": len(oldRows)})
}

// Delete renders DELETE row: conflict-check inbound references, run
// before-hooks, delete, schedule after-hooks.
func (s *WriteService) Delete(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	pkVals, err := DecodeKey(t, r.PathValue("pk"))
	if err != nil {
		writeErr(w, 400, "VALIDATION", "bad row key", err.Error())
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	oldRows, err := a.FetchByKey(t.Schema, t.PhysTab, t.Keys, pkVals, realColNames(t.Columns))
	if err != nil {
		writeErr(w, 502, "CONN", "fetch old failed", err.Error())
		return
	}
	if len(oldRows) == 0 {
		writeErr(w, 404, "NOT_FOUND", "row not found", nil)
		return
	}
	var conflicts []map[string]any
	for _, old := range oldRows {
		cs, err := s.referencedBy(t, old)
		if err != nil {
			writeErr(w, 502, "CONN", "reference check failed", err.Error())
			return
		}
		conflicts = mergeConflicts(conflicts, cs)
	}
	if len(conflicts) > 0 {
		writeErr(w, 409, "CONFLICT", "row is referenced by other rows", conflicts)
		return
	}
	if err := s.guard(t); err != nil {
		writeHookErr(w, err)
		return
	}
	for _, old := range oldRows {
		if _, err := s.runBefore(BeforeDelete, t, RowPayload{Old: old}); err != nil {
			writeHookErr(w, err)
			return
		}
	}
	if _, err := a.DeleteByKey(t.Schema, t.PhysTab, t.Keys, pkVals); err != nil {
		if a.IsFKViolation(err) {
			writeErr(w, 409, "CONFLICT", "row is referenced by other rows (database constraint)", nil)
			return
		}
		writeErr(w, 502, "CONN", "delete failed", err.Error())
		return
	}
	// audit returns in Task 11 (platformhooks)
	for _, old := range oldRows {
		s.runAfter(AfterDelete, t, RowPayload{Old: old})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "affected": len(oldRows)})
}

// bulkDeleteCap bounds one bulk-delete request; clients chunk beyond this.
const bulkDeleteCap = 1000

type bulkFailure struct {
	Key     string `json:"key"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

// BulkDelete renders POST rows/bulk-delete: many keys, partial success by
// design — each key is conflict-checked and hook-checked independently,
// mirroring the single-row delete path.
func (s *WriteService) BulkDelete(w http.ResponseWriter, r *http.Request, t *defs.Table) {
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if len(body.Keys) == 0 {
		writeErr(w, 400, "VALIDATION", "keys is required", nil)
		return
	}
	if len(body.Keys) > bulkDeleteCap {
		writeErr(w, 400, "BULK_TOO_LARGE", "bulk delete is limited to 1000 keys per request", nil)
		return
	}
	if err := s.guard(t); err != nil {
		writeHookErr(w, err)
		return
	}
	a, err := s.R.Adapter(t)
	if err != nil {
		writeLiveErr(w, err)
		return
	}
	defer a.Close()

	deleted := 0
	var failures []bulkFailure
	var deletedOlds []map[string]any
	seen := map[string]bool{}
	for _, key := range body.Keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		pkVals, err := DecodeKey(t, key)
		if err != nil {
			failures = append(failures, bulkFailure{Key: key, Code: "VALIDATION", Message: "bad row key"})
			continue
		}
		oldRows, err := a.FetchByKey(t.Schema, t.PhysTab, t.Keys, pkVals, realColNames(t.Columns))
		if err != nil {
			failures = append(failures, bulkFailure{Key: key, Code: "CONN", Message: "fetch failed"})
			continue
		}
		if len(oldRows) == 0 {
			failures = append(failures, bulkFailure{Key: key, Code: "NOT_FOUND", Message: "row not found"})
			continue
		}
		var conflicts []map[string]any
		for _, old := range oldRows {
			cs, err := s.referencedBy(t, old)
			if err != nil {
				failures = append(failures, bulkFailure{Key: key, Code: "CONN", Message: "reference check failed"})
				conflicts = nil
				break
			}
			conflicts = mergeConflicts(conflicts, cs)
		}
		if conflicts != nil {
			failures = append(failures, bulkFailure{Key: key, Code: "CONFLICT",
				Message: "row is referenced by other rows", Detail: conflicts})
			continue
		}
		blocked := false
		for _, old := range oldRows {
			if _, err := s.runBefore(BeforeDelete, t, RowPayload{Old: old}); err != nil {
				failures = append(failures, bulkFailure{Key: key, Code: "HOOK_REJECTED", Message: err.Error()})
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if _, err := a.DeleteByKey(t.Schema, t.PhysTab, t.Keys, pkVals); err != nil {
			code, msg := "CONN", "delete failed"
			if a.IsFKViolation(err) {
				code, msg = "CONFLICT", "row is referenced by other rows (database constraint)"
			}
			failures = append(failures, bulkFailure{Key: key, Code: code, Message: msg})
			continue
		}
		for _, old := range oldRows {
			// audit returns in Task 11 (platformhooks)
			deletedOlds = append(deletedOlds, old)
		}
		deleted++
	}
	for _, old := range deletedOlds {
		s.runAfter(AfterDelete, t, RowPayload{Old: old})
	}
	if failures == nil {
		failures = []bulkFailure{}
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted, "failed": len(failures), "failures": failures})
}
