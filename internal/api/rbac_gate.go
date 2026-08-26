package api

import (
	"errors"
	"net/http"

	"github.com/luthfi9251/kucrud-core/httpapi"
)

// opGrant maps each core/httpapi op onto the platform grant it requires.
// The endpoint-by-endpoint mapping mirrors the old per-handler checks:
//
//	GET  rows, rows/{pk}, rows/export → read   ("no read access")
//	POST rows                        → create
//	PUT  rows/{pk}                   → update
//	DELETE rows/{pk}, bulk-delete    → delete
//	GET  fkoptions, m2moptions, m2m  → read
//	POST import/preview|apply        → create
//
// OpAction never arrives here (core routes no action endpoint; the v1.9
// action execution stays platform-side with its per-action grant).
var opGrant = map[httpapi.Op]string{
	httpapi.OpRead:   "read",
	httpapi.OpCreate: "create",
	httpapi.OpUpdate: "update",
	httpapi.OpDelete: "delete",
	httpapi.OpExport: "read",
	httpapi.OpImport: "create",
}

// rbacGate adapts platform RBAC to core/httpapi's Gate for one resolved
// definition (the /api/data dispatcher resolves the def by name per
// request, so the def id is captured here): admins pass implicitly,
// everyone else needs the stored per-table grant for the op's action —
// exactly the old hasTablePerm checks, 403 body included. The
// QUERY_READONLY guard stays upstream of the gate (core dispatch order).
func (s *Server) rbacGate(u CtxUser, defID int64) httpapi.Gate {
	return func(r *http.Request, op httpapi.Op, table string) error {
		grant, ok := opGrant[op]
		if !ok {
			return errors.New("no access to this table")
		}
		if !s.hasTablePerm(u, defID, grant) {
			return errors.New("no " + grant + " access to this table")
		}
		return nil
	}
}
