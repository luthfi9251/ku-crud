package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"ku-crud/internal/defs"
	"ku-crud/internal/ds"
	"ku-crud/internal/engine"
	"ku-crud/internal/hooks"
	"ku-crud/internal/meta"
)

// metaResolver implements engine.Resolver over the meta store. Definition
// names are persisted table names (defs.Table.Name == TableName); the
// request's own definition wins its name, otherwise the first stored def
// with that name does (duplicate table names across defs are a platform
// config the name-based contract cannot distinguish).
type metaResolver struct {
	s        *Server
	byName   map[string]*meta.TableDef
	idToName map[int64]string
}

func (m *metaResolver) Resolve(name string) (*defs.Table, error) {
	d, ok := m.byName[name]
	if !ok || name == "" {
		return nil, meta.ErrNotFound
	}
	_, cols, err := m.s.store.GetTableDef(d.ID)
	if err != nil {
		return nil, err
	}
	ct := meta.ToCoreDef(*d, cols, m.idToName)
	return &ct, nil
}

func (m *metaResolver) Adapter(t *defs.Table) (ds.Adapter, error) {
	d, ok := m.byName[t.Name]
	if !ok {
		return nil, engine.ErrDSNotFound
	}
	a, err := m.s.liveAdapter(d.DatasourceID)
	if err != nil {
		if errors.Is(err, errDSNotFound) {
			return nil, engine.ErrDSNotFound
		}
		if errors.Is(err, errConn) {
			return nil, engine.ErrConn
		}
		return nil, err
	}
	return a, nil
}

// metaRes builds the meta-backed resolver for one request: every stored
// definition by name, with the request's own definition winning its name.
func (s *Server) metaRes(def *meta.TableDef) *metaResolver {
	res := &metaResolver{s: s, byName: map[string]*meta.TableDef{}, idToName: map[int64]string{}}
	if list, err := s.store.ListTableDefs(); err == nil {
		for i := range list {
			res.idToName[list[i].ID] = list[i].TableName
			if _, taken := res.byName[list[i].TableName]; !taken {
				res.byName[list[i].TableName] = &list[i]
			}
		}
	}
	res.idToName[def.ID] = def.TableName
	res.byName[def.TableName] = def
	return res
}

// readService wires the engine read path for one request: the meta-backed
// resolver, the perm-checked fk filter callback and the per-target read
// grant predicate (the engine itself stays auth-free). Returns the service
// plus the request def converted to the core contract with fk/m2m names.
func (s *Server) readService(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef) (*engine.ReadService, *defs.Table) {
	res := s.metaRes(def)
	ct := meta.ToCoreDef(*def, cols, res.idToName)
	return &engine.ReadService{
		R:      res,
		FKJoin: s.fkJoin(u, def, cols),
		CanRead: func(name string) bool {
			d, ok := res.byName[name]
			return ok && s.hasTablePerm(u, d.ID, "read")
		},
	}, &ct
}

// hookAdapter maps the engine's synchronous Hooks contract onto the
// platform: Guard checks a definition's assignments against this binary's
// registry, RunBefore executes compiled-in before-hooks (request context
// on the request's own table, background elsewhere — the m2m link sync
// always ran detached), RunAfter enqueues one outbox entry per after-hook
// assignment for the worker.
type hookAdapter struct {
	s        *Server
	u        CtxUser
	req      *http.Request
	mainDef  *meta.TableDef
	mainCols []meta.ColumnDef
	res      *metaResolver
}

// defCols resolves a core table back to its stored definition; the
// request's own definition wins its name.
func (h *hookAdapter) defCols(t *defs.Table) (*meta.TableDef, []meta.ColumnDef, error) {
	if t.Name == h.mainDef.TableName {
		return h.mainDef, h.mainCols, nil
	}
	d, ok := h.res.byName[t.Name]
	if !ok {
		return nil, nil, meta.ErrNotFound
	}
	return h.s.store.GetTableDef(d.ID)
}

func (h *hookAdapter) toHookErr(err error) error {
	var me *hooks.MissingError
	return &engine.HookError{Missing: errors.As(err, &me), Msg: err.Error()}
}

func (h *hookAdapter) Guard(t *defs.Table) error {
	def, _, err := h.defCols(t)
	if err != nil {
		return h.toHookErr(err)
	}
	if err := h.s.hookGuard(def); err != nil {
		return h.toHookErr(err)
	}
	return nil
}

func (h *hookAdapter) RunBefore(ev engine.Event, t *defs.Table, row engine.RowPayload) (engine.RowPayload, error) {
	def, cols, err := h.defCols(t)
	if err != nil {
		return row, h.toHookErr(err)
	}
	ctx := context.Background()
	if t.Name == h.mainDef.TableName {
		ctx = h.req.Context()
	}
	out, err := h.s.runBefore(ctx, h.u, def, cols, hooks.Event(ev), row.Values, row.Old)
	if err != nil {
		return row, h.toHookErr(err)
	}
	return engine.RowPayload{Values: out, Old: row.Old}, nil
}

func (h *hookAdapter) RunAfter(ev engine.Event, t *defs.Table, row engine.RowPayload) error {
	def, _, err := h.defCols(t)
	if err != nil {
		return nil // best-effort, mirrors enqueueAfter's swallow-on-error
	}
	h.s.enqueueAfter(h.u, def, hooks.Event(ev), row.Old, row.Values)
	return nil
}

// writeService wires the engine write path for one request: the meta
// resolver, the platform hook adapter (before-hooks synchronous,
// after-hooks via outbox), the junction-grant predicate and inbound-fk
// discovery. The engine writes no audit rows — audit returns as an
// AfterX hook (platformhooks, Task 11).
func (s *Server) writeService(u CtxUser, r *http.Request, def *meta.TableDef, cols []meta.ColumnDef) (*engine.WriteService, *defs.Table) {
	res := s.metaRes(def)
	ha := &hookAdapter{s: s, u: u, req: r, mainDef: def, mainCols: cols, res: res}
	ct := meta.ToCoreDef(*def, cols, res.idToName)
	return &engine.WriteService{
		R: res,
		H: ha,
		CanWrite: func(name, grant string) bool {
			d, ok := res.byName[name]
			return ok && s.hasTablePerm(u, d.ID, grant)
		},
		RefSources: func(t *defs.Table) ([]engine.RefSource, error) {
			d, ok := res.byName[t.Name]
			if !ok {
				return nil, meta.ErrNotFound
			}
			srcs, err := s.store.FKRefSources(d.ID)
			if err != nil {
				return nil, err
			}
			out := make([]engine.RefSource, 0, len(srcs))
			for _, src := range srcs {
				sd, scols, err := s.store.GetTableDef(src.DefID)
				if err != nil {
					return nil, err
				}
				sct := meta.ToCoreDef(*sd, scols, res.idToName)
				out = append(out, engine.RefSource{Src: &sct,
					Column: src.Column, RefColumn: src.RefColumn, Label: src.DefLabel})
			}
			return out, nil
		},
	}, &ct
}

// importService wires the engine import path for one request: the meta
// resolver and the platform hook adapter (guard up-front, before-hooks
// synchronous, after-hooks via outbox) — the same adapter the write path
// uses, so import hooks keep their historical execution semantics.
func (s *Server) importService(r *http.Request, def *meta.TableDef, cols []meta.ColumnDef) (*engine.ImportService, *defs.Table) {
	u := userFrom(r)
	res := s.metaRes(def)
	ha := &hookAdapter{s: s, u: u, req: r, mainDef: def, mainCols: cols, res: res}
	ct := meta.ToCoreDef(*def, cols, res.idToName)
	return &engine.ImportService{R: res, H: ha}, &ct
}

// fkJoin builds the fk-filter callback for one request: the read grant on
// the fk target and its physical names (the impure parts around the pure
// engine parser).
func (s *Server) fkJoin(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef) engine.FKJoinResolver {
	byName := map[string]meta.ColumnDef{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	return func(column string) (*ds.FKJoin, error) {
		c := byName[column]
		targetID := c.FKTableDefID
		if targetID == meta.SelfRef || targetID == def.ID {
			targetID = def.ID
		}
		if !s.hasTablePerm(u, targetID, "read") {
			return nil, fmt.Errorf("filter: column %q requires read access to its target table", column)
		}
		var schema, table string
		if targetID == def.ID {
			schema, table = def.SchemaName, def.TableName
		} else {
			tdef, _, err := s.store.GetTableDef(targetID)
			if err != nil {
				return nil, fmt.Errorf("filter: column %q target definition not found", column)
			}
			schema, table = tdef.SchemaName, tdef.TableName
		}
		return &ds.FKJoin{Schema: schema, Table: table,
			RefColumn: c.FKRefColumn, DisplayColumns: c.FKDisplayColumns}, nil
	}
}
