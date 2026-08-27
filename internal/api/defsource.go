package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/luthfi9251/ku-crud/core/defs"
	"github.com/luthfi9251/ku-crud/core/ds"
	"github.com/luthfi9251/ku-crud/core/httpapi"
	"ku-crud/internal/meta"
)

// The /api/data/{name}... namespace: platform data endpoints served by
// core/httpapi over meta-backed definitions. Definitions are
// runtime-mutable (the wizard), so nothing is cached — each request
// resolves its def by name (first stored def with that table name wins,
// the metaRes semantics), converts it to the core contract and hands
// core/httpapi the engine services wired exactly like the old platform
// handlers (perm-checked fk joins, per-target read grants, junction write
// grants, metadata ref sources, the outbox-backed hook adapter).

// defByName resolves a definition for the /api/data namespace: the first
// stored def whose table name matches wins (the metaRes semantics) —
// except query views, which have no table name by design and are
// addressed by their masked def-id token instead. A token only ever
// resolves to a NAMELESS def: a named def is found by its name first, so
// tokens and table names cannot shadow each other.
func (s *Server) defByName(name string) (*meta.TableDef, []meta.ColumnDef, error) {
	if name == "" {
		return nil, nil, meta.ErrNotFound
	}
	list, err := s.store.ListTableDefs()
	if err != nil {
		return nil, nil, err
	}
	for i := range list {
		if list[i].TableName == name {
			return s.store.GetTableDef(list[i].ID)
		}
	}
	if id, err := s.ids.Decode("td", name); err == nil {
		if def, cols, err := s.store.GetTableDef(id); err == nil && def.TableName == "" {
			return def, cols, nil
		}
	}
	return nil, nil, meta.ErrNotFound
}

// metaDefSource adapts the per-request metaResolver to core/httpapi's
// DefSource. With Services injected (below) the source only satisfies
// the constructor's slot — core's default wiring never runs.
type metaDefSource struct{ res *metaResolver }

func (m *metaDefSource) Resolve(name string) (*defs.Table, error) { return m.res.Resolve(name) }
func (m *metaDefSource) Adapter(t *defs.Table) (ds.Adapter, error) {
	return m.res.Adapter(t)
}
func (m *metaDefSource) Defs() []*defs.Table {
	out := make([]*defs.Table, 0, len(m.res.byName))
	for _, d := range m.res.byName {
		if ct, err := m.res.Resolve(d.TableName); err == nil {
			out = append(out, ct)
		}
	}
	return out
}

// handleData serves /api/data/{name}[/{route}...]:
//
//	GET  /api/data/{name}                 → def JSON (data-page shape,
//	                                        byte-compatible with the old
//	                                        GET /api/tables/{id})
//	POST /api/data/{name}/rows/{pk}/action → v1.9 action execution
//	                                        (platform-owned, stays here)
//	/api/data/{name}/rows|fkoptions|m2moptions|import/... → core/httpapi
//
// The core resource is built per request (defs are runtime-mutable via
// the wizard) with the engine services wired exactly like the old
// platform handlers — one shared meta resolver, perm-checked fk joins,
// per-target read grants, junction write grants, metadata ref sources,
// the outbox-backed hook adapter — and the request URL is rewritten to
// the def-relative tail so def names that collide with route anchors
// ("rows", "import", ...) cannot misroute.
func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/api/data/")
	segs := strings.Split(rest, "/")
	name := segs[0]
	if u, err := url.PathUnescape(name); err == nil {
		name = u
	}
	if name == "" {
		writeErr(w, 404, "NOT_FOUND", "route not found", nil)
		return
	}
	tail := ""
	if len(segs) > 1 {
		for i := 1; i < len(segs); i++ {
			if u, err := url.PathUnescape(segs[i]); err == nil {
				segs[i] = u
			}
		}
		tail = "/" + strings.Join(segs[1:], "/")
	}

	// def JSON by name for the data pages (the wizard keeps the
	// id-addressed management endpoint); nameless query defs arrive by
	// token and resolve identically
	if tail == "" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeErr(w, 405, "METHOD_NOT_ALLOWED", "method not allowed", nil)
			return
		}
		def, cols, err := s.defByName(name)
		if err != nil {
			s.writeDefErr(w, err)
			return
		}
		u := userFrom(r)
		p := s.tablePerms(u, def)
		if !u.ManageTables && !p.Read {
			writeErr(w, 403, "FORBIDDEN", "no access to this table", nil)
			return
		}
		writeJSON(w, 200, s.toTableDTO(def, cols, p, s.groupNameMap()))
		return
	}

	// v1.9 action execution: core has no action route, so the platform
	// serves it out of the same namespace before delegating
	if t := strings.Split(strings.Trim(tail, "/"), "/"); len(t) == 3 &&
		t[0] == "rows" && t[2] == "action" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeErr(w, 405, "METHOD_NOT_ALLOWED", "method not allowed", nil)
			return
		}
		def, cols, err := s.defByName(name)
		if err != nil {
			s.writeDefErr(w, err)
			return
		}
		r.SetPathValue("pk", t[1])
		s.runRowAction(w, r, def, cols)
		return
	}

	def, cols, err := s.defByName(name)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	u := userFrom(r)
	res := s.metaRes(def)
	read, ct := s.readService(u, res, def, cols)
	write, _ := s.writeService(u, r, res, def, cols)
	imp, _ := s.importService(r, res, def, cols)
	h := httpapi.New(def.TableName, ct, &metaDefSource{res: res}, httpapi.Options{
		Gate: s.rbacGate(u, def.ID),
		Services: func(r *http.Request, t *defs.Table) httpapi.ServiceSet {
			return httpapi.ServiceSet{Read: read, Write: write, Import: imp}
		},
	})
	// rewrite to the def-relative tail on a cloned request: the resource
	// must see /rows/... even when the def name is itself an anchor word
	r2 := r.Clone(r.Context())
	u2 := *r.URL
	u2.Path, u2.RawPath = tail, ""
	r2.URL = &u2
	h.ServeHTTP(w, r2)
}
