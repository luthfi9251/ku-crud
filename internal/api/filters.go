package api

import (
	"github.com/luthfi9251/ku-crud/core/ds"
	"github.com/luthfi9251/ku-crud/core/engine"
	"ku-crud/internal/meta"
)

// parseFilters validates the `filters` query param (JSON array) against the
// stored definition via the pure engine. Only the impure parts remain here:
// the read grant on fk targets and resolving their physical names
// (shared with the engine read path via fkJoin).
func (s *Server) parseFilters(def *meta.TableDef, cols []meta.ColumnDef, u CtxUser, raw string) ([]ds.ColumnFilter, string) {
	f, err := engine.ParseFilters(toCore(def, cols), raw, s.fkJoin(u, def, cols))
	if err != nil {
		return nil, err.Error()
	}
	return f, ""
}
