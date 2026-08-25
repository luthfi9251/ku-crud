package api

import (
	"errors"
	"fmt"

	"ku-crud/internal/defs"
	"ku-crud/internal/ds"
	"ku-crud/internal/engine"
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

// readService wires the engine read path for one request: the meta-backed
// resolver, the perm-checked fk filter callback and the per-target read
// grant predicate (the engine itself stays auth-free). Returns the service
// plus the request def converted to the core contract with fk/m2m names.
func (s *Server) readService(u CtxUser, def *meta.TableDef, cols []meta.ColumnDef) (*engine.ReadService, *defs.Table) {
	res := &metaResolver{s: s, byName: map[string]*meta.TableDef{}, idToName: map[int64]string{}}
	if list, err := s.store.ListTableDefs(); err == nil {
		for i := range list {
			res.idToName[list[i].ID] = list[i].TableName
			if _, taken := res.byName[list[i].TableName]; !taken {
				res.byName[list[i].TableName] = &list[i]
			}
		}
	}
	// the request's def wins its own name (self-fk and the main adapter)
	res.idToName[def.ID] = def.TableName
	res.byName[def.TableName] = def
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
