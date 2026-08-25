package api

import (
	"fmt"

	"ku-crud/internal/ds"
	"ku-crud/internal/engine"
	"ku-crud/internal/meta"
)

// parseFilters validates the `filters` query param (JSON array) against the
// stored definition via the pure engine. Only the impure parts remain here:
// the read grant on fk targets and resolving their physical names.
func (s *Server) parseFilters(def *meta.TableDef, cols []meta.ColumnDef, u CtxUser, raw string) ([]ds.ColumnFilter, string) {
	byName := map[string]meta.ColumnDef{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	f, err := engine.ParseFilters(toCore(def, cols), raw, func(column string) (*ds.FKJoin, error) {
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
	})
	if err != nil {
		return nil, err.Error()
	}
	return f, ""
}
