// Package httpapi renders kucrud definitions as mount-anywhere HTTP
// handlers: the name-based data routes (rows CRUD, fk/m2m pickers, CSV
// export/import) mirroring the platform's data endpoints, plus the
// App-level /api/defs listing. Handlers own no server, router or auth —
// the host mounts them behind its own middleware; the Gate func is the
// single per-op authorization slot.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
	"github.com/luthfi9251/kucrud-core/engine"
	"github.com/luthfi9251/kucrud-core/hooks"
)

// Op names the operation class of one request. Gates receive it per op.
type Op string

const (
	OpRead   Op = "read"
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"
	// OpAction is reserved for v1.9 custom row actions; the engine has no
	// action execution yet, so no core route dispatches it.
	OpAction Op = "action"
	OpExport Op = "export"
	OpImport Op = "import"
)

// Gate is the single auth/RBAC slot: return non-nil to reject the request
// with 403 (message from err). table is the def name.
type Gate func(r *http.Request, op Op, table string) error

// DefSource supplies the registered definitions behind a resource: name
// resolution, the shared physical adapter and the ordered def list. The
// App implements it; it structurally satisfies engine.Resolver.
type DefSource interface {
	Resolve(name string) (*defs.Table, error)
	Adapter(t *defs.Table) (ds.Adapter, error)
	Defs() []*defs.Table
}

// ServiceSet bundles the engine services one request runs on. Hosts that
// own their wiring (per-target read grants, junction write grants,
// metadata-derived ref sources, an outbox-backed hook adapter) supply the
// full set; the Resource itself only routes, guards and gates.
type ServiceSet struct {
	Read   *engine.ReadService
	Write  *engine.WriteService
	Import *engine.ImportService
}

// Options configures one Resource handler.
type Options struct {
	// Gate, when non-nil, is called before every op.
	Gate Gate
	// Registry resolves hook names; nil means hooks.Default.
	Registry *hooks.Registry
	// Services, when non-nil, replaces the default engine wiring for every
	// op — the advanced path for hosts whose resolver spans multiple
	// connections or whose hook execution is not registry-synchronous (the
	// platform is the first consumer: meta-backed defs, perm-checked fk
	// joins, async after-hooks via outbox). Nil wires the DefSource
	// defaults.
	Services func(r *http.Request, t *defs.Table) ServiceSet
}

// Resource is a plain http.Handler serving ONE definition's data routes.
// Routes are RELATIVE to the host's mount point: the handler finds its
// anchor segment (rows/fkoptions/m2moptions/import) wherever it appears
// in the URL, so it can be mounted under any prefix — but a mount prefix
// must not itself contain a segment named rows, fkoptions, m2moptions or
// import. It does not serve /defs (that lives on the App mux only).
type Resource struct {
	name string
	t    *defs.Table
	src  DefSource
	gate Gate
	reg  *hooks.Registry
	svc  func(r *http.Request, t *defs.Table) ServiceSet
}

// New builds the def's HTTP handler. The table must come fully built
// (introspected defaults + overrides merged — App.Resource does that).
func New(name string, t *defs.Table, src DefSource, o Options) *Resource {
	reg := o.Registry
	if reg == nil {
		reg = hooks.Default
	}
	return &Resource{name: name, t: t, src: src, gate: o.Gate, reg: reg, svc: o.Services}
}

func writeErr(w http.ResponseWriter, status int, code, msg string, detail any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": msg, "detail": detail})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeErr(w, 405, "METHOD_NOT_ALLOWED", "method not allowed", nil)
}

// anchors are the route roots a Resource serves; everything before the
// first anchor in the URL path is the host's mount prefix.
var anchors = map[string]bool{
	"rows": true, "fkoptions": true, "m2moptions": true, "import": true,
}

// routePath extracts the def-independent sub-path from a mounted URL:
// the tail starting at the first anchor segment ("/host/prefix/rows/1"
// → "/rows/1"). ok=false means no route of this resource is addressed.
func routePath(p string) (string, bool) {
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", false
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range segs {
		if anchors[s] {
			return "/" + strings.Join(segs[i:], "/"), true
		}
	}
	return "", false
}

// services wires the engine services for one request. They are built per
// request so the hook adapter can thread the request context (and with it
// the actor the host may have injected via hooks.WithActor). A host
// Services func replaces the default wiring wholesale.
func (h *Resource) services(r *http.Request) (read *engine.ReadService,
	write *engine.WriteService, imp *engine.ImportService,
) {
	if h.svc != nil {
		ss := h.svc(r, h.t)
		return ss.Read, ss.Write, ss.Import
	}
	var hh engine.Hooks
	if h.reg != nil {
		hh = &hookAdapter{src: h.src, reg: h.reg, r: r}
	}
	return &engine.ReadService{R: h.src, FKJoin: h.fkJoin},
		&engine.WriteService{R: h.src, H: hh, RefSources: h.refSources},
		&engine.ImportService{R: h.src, H: hh}
}

// dispatch applies the query-view write guard and the Gate, then runs fn.
// write marks ops the platform rejects on query views before its own
// grant checks; the guard keeps that order (guard, then gate).
func (h *Resource) dispatch(w http.ResponseWriter, r *http.Request,
	op Op, write bool, fn func(),
) {
	if write && h.t.SourceType == "query" {
		writeErr(w, 403, "QUERY_READONLY", "query views are read-only", nil)
		return
	}
	if h.gate != nil {
		if err := h.gate(r, op, h.name); err != nil {
			writeErr(w, 403, "FORBIDDEN", err.Error(), nil)
			return
		}
	}
	fn()
}

func (h *Resource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub, ok := routePath(r.URL.EscapedPath())
	if !ok {
		writeErr(w, 404, "NOT_FOUND", "route not found", nil)
		return
	}
	segs := strings.Split(strings.TrimPrefix(sub, "/"), "/")
	for i := range segs { // mirror ServeMux: unescape each matched segment
		if u, err := url.PathUnescape(segs[i]); err == nil {
			segs[i] = u
		}
	}
	setPK := func(v string) { r.SetPathValue("pk", v) }
	setCol := func(v string) { r.SetPathValue("column", v) }

	switch segs[0] {
	case "rows":
		switch {
		case len(segs) == 1:
			switch r.Method {
			case http.MethodGet:
				read, _, _ := h.services(r)
				h.dispatch(w, r, OpRead, false, func() { read.List(w, r, h.t) })
			case http.MethodPost:
				_, write, _ := h.services(r)
				h.dispatch(w, r, OpCreate, true, func() { write.Insert(w, r, h.t) })
			default:
				writeMethodNotAllowed(w, "GET, POST")
			}
		case len(segs) == 2 && segs[1] == "export":
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w, "GET")
				return
			}
			read, _, _ := h.services(r)
			h.dispatch(w, r, OpExport, false, func() { read.ExportCSV(w, r, h.t) })
		case len(segs) == 2 && segs[1] == "bulk-delete":
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, "POST")
				return
			}
			_, write, _ := h.services(r)
			h.dispatch(w, r, OpDelete, true, func() { write.BulkDelete(w, r, h.t) })
		case len(segs) == 2:
			pk := segs[1]
			switch r.Method {
			case http.MethodGet:
				read, _, _ := h.services(r)
				h.dispatch(w, r, OpRead, false, func() { setPK(pk); read.Get(w, r, h.t) })
			case http.MethodPut:
				_, write, _ := h.services(r)
				h.dispatch(w, r, OpUpdate, true, func() { setPK(pk); write.Update(w, r, h.t) })
			case http.MethodDelete:
				_, write, _ := h.services(r)
				h.dispatch(w, r, OpDelete, true, func() { setPK(pk); write.Delete(w, r, h.t) })
			default:
				writeMethodNotAllowed(w, "GET, PUT, DELETE")
			}
		case len(segs) == 4 && segs[2] == "m2m":
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w, "GET")
				return
			}
			pk, col := segs[1], segs[3]
			read, _, _ := h.services(r)
			// write-guarded read: query views reject relation endpoints
			// outright (they cannot declare fk/m2m columns), matching the
			// host's historical guard-before-grant order
			h.dispatch(w, r, OpRead, true, func() { setPK(pk); setCol(col); read.M2MLinks(w, r, h.t) })
		default:
			writeErr(w, 404, "NOT_FOUND", "route not found", nil)
		}
	case "fkoptions":
		if len(segs) != 2 {
			writeErr(w, 404, "NOT_FOUND", "route not found", nil)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, "GET")
			return
		}
		col := segs[1]
		read, _, _ := h.services(r)
		h.dispatch(w, r, OpRead, true, func() { setCol(col); read.FKOptions(w, r, h.t) })
	case "m2moptions":
		if len(segs) != 2 {
			writeErr(w, 404, "NOT_FOUND", "route not found", nil)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, "GET")
			return
		}
		col := segs[1]
		read, _, _ := h.services(r)
		h.dispatch(w, r, OpRead, true, func() { setCol(col); read.M2MOptions(w, r, h.t) })
	case "import":
		if len(segs) != 2 || (segs[1] != "preview" && segs[1] != "apply") {
			writeErr(w, 404, "NOT_FOUND", "route not found", nil)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, "POST")
			return
		}
		stage := segs[1]
		_, _, imp := h.services(r)
		h.dispatch(w, r, OpImport, true, func() {
			if stage == "preview" {
				imp.PreviewImport(w, r, h.t)
				return
			}
			imp.ApplyImport(w, r, h.t)
		})
	default:
		writeErr(w, 404, "NOT_FOUND", "route not found", nil)
	}
}

// fkJoin resolves one fk column's filter join target (physical names of
// the fk target definition). Core has no per-target read grants — the
// Gate is the single auth slot — so this only fails on configuration.
func (h *Resource) fkJoin(column string) (*ds.FKJoin, error) {
	for _, c := range h.t.Columns {
		if c.Name != column {
			continue
		}
		if c.FieldType != "fk" || c.FK == nil {
			return nil, fmt.Errorf("filter: column %q is not an fk column", column)
		}
		target := h.t
		if c.FK.Table != "" {
			resolved, err := h.src.Resolve(c.FK.Table)
			if err != nil {
				return nil, fmt.Errorf("filter: column %q target definition not found", column)
			}
			target = resolved
		}
		return &ds.FKJoin{Schema: target.Schema, Table: target.PhysTab,
			RefColumn: c.FK.RefColumn, DisplayColumns: c.FK.DisplayColumns}, nil
	}
	return nil, fmt.Errorf("filter: unknown column %q", column)
}

// refSources lists registered definitions whose fk columns target t
// (delete protection), mirroring the platform's metadata-derived sources.
func (h *Resource) refSources(t *defs.Table) ([]engine.RefSource, error) {
	var out []engine.RefSource
	for _, d := range h.src.Defs() {
		if d.Name == t.Name {
			continue
		}
		for _, c := range d.Columns {
			if c.FieldType == "fk" && c.FK != nil && c.FK.Table == t.Name {
				out = append(out, engine.RefSource{Src: d, Column: c.Name,
					RefColumn: c.FK.RefColumn, Label: d.Label})
			}
		}
	}
	return out, nil
}

// hookAdapter maps the engine's synchronous Hooks contract onto the hook
// registry: Guard checks assignments against the registry (missing-hook
// rejection), RunBefore executes before-hooks synchronously with the
// request context (carrying any actor the host injected), RunAfter runs
// after-hooks synchronously post-commit — the library has no outbox
// worker; the platform keeps its async outbox semantics.
type hookAdapter struct {
	src DefSource
	reg *hooks.Registry
	r   *http.Request
}

func (h *hookAdapter) assignments(t *defs.Table) (hooks.Assignments, error) {
	return hooks.ParseAssignments(t.Hooks)
}

func (h *hookAdapter) Guard(t *defs.Table) error {
	asgs, err := h.assignments(t)
	if err != nil {
		return err
	}
	return h.reg.CheckMissing(asgs)
}

func (h *hookAdapter) ctx(t *defs.Table) (context.Context, *hooks.HookContext) {
	ctx := context.Background()
	if h.r != nil {
		ctx = h.r.Context()
	}
	hc := &hooks.HookContext{Table: t, Columns: t.Columns, Logger: slog.Default(),
		Open: func(name string) (ds.Adapter, error) {
			dt, err := h.src.Resolve(name)
			if err != nil {
				return nil, fmt.Errorf("unknown table definition %q", name)
			}
			return h.src.Adapter(dt)
		}}
	hc.Actor = hooks.ActorFrom(ctx)
	return ctx, hc
}

func (h *hookAdapter) RunBefore(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error) {
	asgs, err := h.assignments(t)
	if err != nil {
		return row, err
	}
	if len(asgs[ev]) == 0 {
		return row, nil
	}
	ctx, hc := h.ctx(t)
	return h.reg.RunBefore(ctx, ev, asgs[ev], hc, row)
}

func (h *hookAdapter) RunAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload) error {
	asgs, err := h.assignments(t)
	if err != nil || len(asgs[ev]) == 0 {
		return nil // best-effort by contract
	}
	ctx, hc := h.ctx(t)
	for _, a := range asgs[ev] {
		if err := h.reg.RunOne(ctx, hc, ev, row, a); err != nil {
			slog.Warn("after-hook failed", "event", ev, "table", t.Name,
				"hook", a.Hook, "err", err.Error())
		}
	}
	return nil
}

// ---- /api/defs ----

type PermsDTO struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}

type FKDTO struct {
	Table          string   `json:"table"` // target def name; "" = self
	RefColumn      string   `json:"refColumn"`
	DisplayColumns []string `json:"displayColumns,omitempty"`
}

type M2MDTO struct {
	JunctionTable  string   `json:"junctionTable"`
	SrcCol         string   `json:"srcCol"`
	TgtCol         string   `json:"tgtCol"`
	DisplayColumns []string `json:"displayColumns,omitempty"`
}

type ColumnDTO struct {
	Name            string                `json:"name"`
	Label           string                `json:"label"`
	FieldType       string                `json:"fieldType"`
	EnumOptions     []string              `json:"enumOptions,omitempty"`
	Editable        bool                  `json:"editable"`
	Required        bool                  `json:"required"`
	Visible         bool                  `json:"visible"`
	Searchable      bool                  `json:"searchable"`
	Sortable        bool                  `json:"sortable"`
	Position        int                   `json:"position"`
	Validations     []defs.ValidationRule `json:"validations,omitempty"`
	BaseType        string                `json:"baseType,omitempty"`
	FK              *FKDTO                `json:"fk,omitempty"`
	M2M             *M2MDTO               `json:"m2m,omitempty"`
	IsComputed      bool                  `json:"isComputed,omitempty"`
	ComputedFormula string                `json:"computedFormula,omitempty"`
	Formatting      json.RawMessage       `json:"formatting,omitempty"`
	M2MRefColumn    string                `json:"m2mRefColumn,omitempty"`
	M2MTargetRef    string                `json:"m2mTargetRef,omitempty"`
}

type DefDTO struct {
	Name           string          `json:"name"`
	Label          string          `json:"label"`
	Description    string          `json:"description,omitempty"`
	Schema         string          `json:"schema,omitempty"`
	Table          string          `json:"table,omitempty"`
	Keys           []string        `json:"keyColumns"`
	PageSize       int             `json:"pageSize"`
	DefaultSortCol string          `json:"defaultSortCol,omitempty"`
	DefaultSortDir string          `json:"defaultSortDir,omitempty"`
	SourceType     string          `json:"sourceType,omitempty"`
	QuerySQL       string          `json:"querySql,omitempty"`
	Hooks          json.RawMessage `json:"hooks,omitempty"`
	Actions        json.RawMessage `json:"actions,omitempty"`
	Columns        []ColumnDTO     `json:"columns"`
	Permissions    PermsDTO        `json:"permissions"`
}

func defToDTO(src DefSource, t *defs.Table, p PermsDTO) DefDTO {
	dto := DefDTO{Name: t.Name, Label: t.Label, Description: t.Description,
		Schema: t.Schema, Table: t.PhysTab, Keys: t.Keys, PageSize: t.PageSize,
		DefaultSortCol: t.DefaultSortCol, DefaultSortDir: t.DefaultSortDir,
		SourceType: t.SourceType, QuerySQL: t.QuerySQL,
		Actions: rawOrNull(t.Actions), Permissions: p}
	if dto.Keys == nil {
		dto.Keys = []string{}
	}
	if h := rawOrNull(t.Hooks); h != nil {
		dto.Hooks = h
	}
	for _, c := range t.Columns {
		col := ColumnDTO{Name: c.Name, Label: c.Label, FieldType: c.FieldType,
			EnumOptions: c.EnumOptions, Editable: c.Editable, Required: c.Required,
			Visible: c.Visible, Searchable: c.Searchable, Sortable: c.Sortable,
			Position: c.Position, Validations: c.Validations, BaseType: c.BaseType,
			IsComputed: c.IsComputed, ComputedFormula: c.ComputedFormula}
		if c.Formatting != "" {
			col.Formatting = json.RawMessage(c.Formatting)
		}
		if c.FK != nil {
			col.FK = &FKDTO{Table: c.FK.Table, RefColumn: c.FK.RefColumn,
				DisplayColumns: c.FK.DisplayColumns}
		}
		if c.M2M != nil {
			col.M2M = &M2MDTO{JunctionTable: c.M2M.JunctionTable, SrcCol: c.M2M.SrcCol,
				TgtCol: c.M2M.TgtCol, DisplayColumns: c.M2M.DisplayColumns}
			// resolve the junction's ref columns so grids can key m2m lookups
			// (best-effort: unresolved junctions just omit them)
			if cfg, _ := engine.ResolveM2M(src, t, c); cfg != nil {
				col.M2MRefColumn, col.M2MTargetRef = cfg.SrcRef, cfg.TargetRef
			}
		}
		dto.Columns = append(dto.Columns, col)
	}
	return dto
}

func rawOrNull(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

// DefsHandler serves GET /api/defs: the registered definitions in
// registration order, name-keyed (fk/m2m targets and junctions are def
// names, "" fk target = self). When a Gate is set it is probed per def
// (read/create/update/delete) so the listing reflects the caller's
// access; without a Gate every def reports full permissions and the Gate
// stays the runtime enforcement point.
func DefsHandler(src DefSource, gate Gate) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, "GET")
			return
		}
		list := src.Defs()
		out := make([]DefDTO, 0, len(list))
		for _, t := range list {
			p := PermsDTO{Read: true, Create: true, Update: true, Delete: true}
			if gate != nil {
				p = PermsDTO{
					Read:   gate(r, OpRead, t.Name) == nil,
					Create: gate(r, OpCreate, t.Name) == nil,
					Update: gate(r, OpUpdate, t.Name) == nil,
					Delete: gate(r, OpDelete, t.Name) == nil,
				}
			}
			if t.SourceType == "query" {
				p.Create, p.Update, p.Delete = false, false, false
			}
			out = append(out, defToDTO(src, t, p))
		}
		writeJSON(w, http.StatusOK, out)
	})
}
