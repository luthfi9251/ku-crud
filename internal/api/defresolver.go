package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
	"github.com/luthfi9251/kucrud-core/engine"
	"github.com/luthfi9251/kucrud-core/hooks"
	"ku-crud/internal/hooks/platformhooks"
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

// readService wires the engine read path for one request over a shared
// resolver: the perm-checked fk filter callback and the per-target read
// grant predicate (the engine itself stays auth-free). Returns the service
// plus the request def converted to the core contract with fk/m2m names.
func (s *Server) readService(u CtxUser, res *metaResolver, def *meta.TableDef, cols []meta.ColumnDef) (*engine.ReadService, *defs.Table) {
	ct := meta.ToCoreDef(*def, cols, res.idToName)
	return &engine.ReadService{
		R:      res,
		FKJoin: s.fkJoin(u, def, cols),
		CanRead: func(name string) bool {
			if name == defs.MissingTable {
				// dangling def id: admins keep implicit access (the old
				// hasTablePerm short-circuit); everyone else has no stored
				// grant on a deleted definition
				return u.IsAdmin
			}
			d, ok := res.byName[name]
			return ok && s.hasTablePerm(u, d.ID, "read")
		},
	}, &ct
}

// hookAdapter maps the engine's Hooks contract onto the platform: Guard
// checks a definition's assignments against this binary's registry,
// RunBefore executes compiled-in before-hooks (request context on the
// request's own table, background elsewhere — the m2m link sync always
// ran detached; the actor rides both the context and the hook context),
// RunSyncAfter writes the platform audit trail synchronously inside the
// request — exactly where the pre-extraction handlers called
// auditBestEffort, ahead of RunAfter — and RunAfter enqueues one outbox
// entry per after-hook assignment for the worker. Every definition gets
// the audit hook by adapter construction; nothing is persisted to meta.
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

func (h *hookAdapter) Guard(t *defs.Table) error {
	def, _, err := h.defCols(t)
	if err != nil {
		return err
	}
	return h.s.hookGuard(def)
}

func (h *hookAdapter) RunBefore(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error) {
	ctx := context.Background()
	if t.Name == h.mainDef.TableName {
		ctx = h.req.Context()
	}
	ctx = hooks.WithActor(ctx, h.u.Username)
	out, err := h.s.runBefore(ctx, h.s.hookCtx(h.u.Username, h.res, t), t, ev, row.Values, row.Old)
	if err != nil {
		return row, err
	}
	return hooks.RowPayload{Values: out, Old: row.Old, Message: row.Message}, nil
}

func (h *hookAdapter) RunAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload) error {
	def, _, err := h.defCols(t)
	if err != nil {
		return nil // best-effort, mirrors enqueueAfter's swallow-on-error
	}
	h.s.enqueueAfter(h.u, def, ev, row.Old, row.Values)
	return nil
}

// RunSyncAfter writes the audit entry synchronously (best-effort, never
// fails the request) — the pre-extraction audit timing, before RunAfter
// enqueues the outbox.
func (h *hookAdapter) RunSyncAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload, rowKey string) {
	def, _, err := h.defCols(t)
	if err != nil {
		return // best-effort, mirrors RunAfter's swallow-on-error
	}
	platformhooks.Audit(h.s.store, def.ID, h.u.ID, ev, row, rowKey)
}

// writeService wires the engine write path for one request over a shared
// resolver: the platform hook adapter (before-hooks synchronous, the
// audit trail synchronous-after, user after-hooks via outbox), the
// junction-grant predicate and inbound-fk discovery.
func (s *Server) writeService(u CtxUser, r *http.Request, res *metaResolver, def *meta.TableDef, cols []meta.ColumnDef) (*engine.WriteService, *defs.Table) {
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

// importService wires the engine import path for one request over a
// shared resolver: the platform hook adapter (guard up-front,
// before-hooks synchronous, after-hooks via outbox) — the same adapter
// the write path uses, so import hooks keep their historical execution
// semantics.
func (s *Server) importService(r *http.Request, res *metaResolver, def *meta.TableDef, cols []meta.ColumnDef) (*engine.ImportService, *defs.Table) {
	u := userFrom(r)
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
