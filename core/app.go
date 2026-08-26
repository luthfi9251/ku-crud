// Package kucrud is the code-first CRUD library surface: declare tables
// with introspection-backed defaults and per-column overrides, obtain a
// mount-anywhere http.Handler per definition, and drive everything
// through one connection. The host owns server, router and auth; the Gate
// func is the single authorization slot.
//
// Typical use:
//
//	app, _ := kucrud.New(kucrud.Conn{Driver: "postgres", Raw: dsn},
//		kucrud.WithGate(myGate))
//	h, _ := app.Resource("products", kucrud.Def{Table: "products"})
//	mux.Handle("/api/v1/products/", h)          // mount anywhere
//	app.CRUD("/api/data/orders", kucrud.Def{    // or the App mux wholesale
//		Table: "orders",
//		Columns: []kucrud.Override{{Name: "note", Required: true}},
//	})
//	http.ListenAndServe(":8080", mux)           // + mux.Handle("/api/", app)
package kucrud

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
	"github.com/luthfi9251/kucrud-core/hooks"
	"github.com/luthfi9251/kucrud-core/httpapi"
)

// Conn is the dialect-neutral connection description (raw overrides all).
type Conn = ds.Conn

// Root-package aliases so users write kucrud.FK / kucrud.Sort, not
// kucrud-core/defs.FK.
type (
	FK             = defs.FK
	M2M            = defs.M2M
	SortDir        = defs.SortDir
	ValidationRule = defs.ValidationRule
	Event          = hooks.Event
	HookFunc       = hooks.HookFunc
	RowPayload     = hooks.RowPayload
	HookContext    = hooks.HookContext
)

const (
	Asc  = defs.Asc
	Desc = defs.Desc

	BeforeCreate = hooks.BeforeCreate
	AfterCreate  = hooks.AfterCreate
	BeforeUpdate = hooks.BeforeUpdate
	AfterUpdate  = hooks.AfterUpdate
	BeforeDelete = hooks.BeforeDelete
	AfterDelete  = hooks.AfterDelete
)

// Op is the operation class a Gate sees per request.
type Op = httpapi.Op

// Op constants (read|create|update|delete|action|export|import).
const (
	OpRead   = httpapi.OpRead
	OpCreate = httpapi.OpCreate
	OpUpdate = httpapi.OpUpdate
	OpDelete = httpapi.OpDelete
	OpAction = httpapi.OpAction // reserved: no core route dispatches it yet
	OpExport = httpapi.OpExport
	OpImport = httpapi.OpImport
)

// Gate is the single auth/RBAC slot: return non-nil to reject with 403.
type Gate = httpapi.Gate

// SortSpec is one default-sort declaration.
type SortSpec struct {
	Col string
	Dir defs.SortDir
}

// Sort builds a SortSpec (kucrud.Sort("created_at", kucrud.Desc)).
func Sort(col string, dir defs.SortDir) SortSpec { return SortSpec{Col: col, Dir: dir} }

// Def declares one CRUD resource. For SourceType "table" (the default)
// the physical table's columns are introspected at registration time and
// provide the defaults; Columns are Overrides merged by Name
// (declaration = override, not a full listing). SourceType "query" wraps
// a read-only SELECT (writes return 403 QUERY_READONLY).
type Def struct {
	Table       string
	Keys        []string // default: introspected primary key
	Columns     []Override
	DefaultSort SortSpec
	Hooks       map[Event][]string // hook names in the registry
	Actions     string             // actions JSON (config passthrough; not executed by core)
	SourceType  string             // "table" (default) | "query"
	QuerySQL    string
	PageSize    int // default 20, max 200
}

// Override refines one introspected column. Merge rule: introspection
// provides defaults; an override replaces only non-zero fields — Label,
// Hidden (true → not visible), Format, Validation, FK, M2M, Editable and
// Required replace when truthy, Searchable and Sortable are pointers and
// replace when non-nil. Note the asymmetry (the brief's shape): Editable/
// Required cannot be forced false from here — only the pointer fields
// can switch a default off. FK upgrades a physical column to an fk
// column (BaseType keeps the introspected type); M2M appends a virtual
// relation column (no physical counterpart).
type Override struct {
	Name       string
	Label      string
	Hidden     bool
	Format     string // formatting JSON, existing contract
	Validation []defs.ValidationRule
	FK         *defs.FK
	M2M        *defs.M2M
	Editable   bool
	Required   bool
	Searchable *bool
	Sortable   *bool
}

const defaultPageSize = 20

// App owns one validated connection, the registered definitions and an
// internal mux for the CRUD sugar path. It is safe for concurrent use.
type App struct {
	conn Conn
	a    ds.Adapter
	gate Gate
	reg  *hooks.Registry

	mu    sync.RWMutex
	defs  map[string]*defs.Table
	order []string

	mux *http.ServeMux
	src *appSource
}

// Option configures an App at construction.
type Option func(*App)

// WithGate sets the single auth/RBAC slot; it applies to Resources
// created after the option runs (i.e. pass it to New).
func WithGate(g Gate) Option { return func(a *App) { a.gate = g } }

// WithHookRegistry sets the registry Def.Hooks names resolve against
// (default: hooks.Default, where hookgen-generated code registers).
func WithHookRegistry(r *hooks.Registry) Option { return func(a *App) { a.reg = r } }

// New opens and validates one connection used for introspection at
// registration time and as the adapter behind the App resolver.
func New(c Conn, opts ...Option) (*App, error) {
	if c.Driver == "" {
		c.Driver = inferDriver(c.Raw)
	}
	a, err := ds.Open(c)
	if err != nil {
		return nil, fmt.Errorf("kucrud: connect: %w", err)
	}
	app := &App{conn: c, a: a, reg: hooks.Default,
		defs: map[string]*defs.Table{}, mux: http.NewServeMux()}
	app.src = &appSource{app: app}
	// the gate is read per request so late options still apply
	app.mux.Handle("GET /api/defs", httpapi.DefsHandler(app.src,
		func(r *http.Request, op httpapi.Op, table string) error {
			app.mu.RLock()
			g := app.gate
			app.mu.RUnlock()
			if g == nil {
				return nil
			}
			return g(r, op, table)
		}))
	for _, o := range opts {
		o(app)
	}
	return app, nil
}

// inferDriver picks the driver from a raw DSN's scheme so Conn{Raw: ...}
// works without spelling Driver out.
func inferDriver(raw string) string {
	switch {
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		return "postgres"
	case strings.HasPrefix(raw, "mysql://"):
		return "mysql"
	}
	return ""
}

// Close releases the App's connection.
func (a *App) Close() error { return a.a.Close() }

// Resource is the PRIMARY API: it validates the declaration (table
// exists via introspection for table defs, keys set or derivable),
// builds the introspection-backed definition with overrides merged, and
// returns a plain http.Handler serving that def's data routes relative
// to wherever the host mounts it. Registration also enters the def into
// the App registry (so /api/defs, fk/m2m resolution and the engine
// resolver see it); re-registering the same name replaces the def.
func (a *App) Resource(name string, d Def) (http.Handler, error) {
	t, err := a.build(name, d)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if _, exists := a.defs[name]; !exists {
		a.order = append(a.order, name)
	}
	a.defs[name] = t
	gate, reg := a.gate, a.reg
	a.mu.Unlock()
	return httpapi.New(name, t, a.src, httpapi.Options{Gate: gate, Registry: reg}), nil
}

// CRUD is sugar for the lazy path: Resource under the def name taken
// from the path's last segment, registered on the App's internal mux at
// path+"/" (so /api/data/{name}/... works wholesale when the host mounts
// the App). It panics on registration error — startup config error,
// fail fast. The template uses this.
func (a *App) CRUD(path string, d Def) *App {
	p := strings.TrimSuffix(path, "/")
	name := p[strings.LastIndex(p, "/")+1:]
	if name == "" {
		panic(fmt.Sprintf("kucrud: CRUD(%q): path has no name segment", path))
	}
	h, err := a.Resource(name, d)
	if err != nil {
		panic(fmt.Sprintf("kucrud: CRUD(%q): %v", path, err))
	}
	a.mux.Handle(p+"/", h)
	return a
}

// ServeHTTP serves the App's internal mux (CRUD-mounted resources and
// GET /api/defs). Bare Resource handlers do not serve /defs — it lives
// here only.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

// appSource implements httpapi.DefSource (and thereby engine.Resolver)
// over the App registry: name → def, plus the one shared adapter. The
// engine closes adapters per use, so the shared connection is handed out
// with a no-op Close.
type appSource struct{ app *App }

type sharedAdapter struct{ ds.Adapter }

func (sharedAdapter) Close() error { return nil }

func (s *appSource) Resolve(name string) (*defs.Table, error) {
	s.app.mu.RLock()
	defer s.app.mu.RUnlock()
	t, ok := s.app.defs[name]
	if !ok || name == "" {
		return nil, fmt.Errorf("table definition %q not registered", name)
	}
	return t, nil
}

func (s *appSource) Adapter(*defs.Table) (ds.Adapter, error) {
	return sharedAdapter{s.app.a}, nil
}

func (s *appSource) Defs() []*defs.Table {
	s.app.mu.RLock()
	defer s.app.mu.RUnlock()
	out := make([]*defs.Table, 0, len(s.app.order))
	for _, name := range s.app.order {
		out = append(out, s.app.defs[name])
	}
	return out
}

// ---- registration-time validation and def building ----

var (
	validRules    = map[string]bool{"email": true, "min_len": true, "max_len": true, "number": true, "text": true}
	validEnumCols = map[string]bool{"gray": true, "blue": true, "green": true, "amber": true,
		"red": true, "purple": true, "cyan": true, "orange": true}
)

const querySQLMax = 20000

// humanize mirrors the wizard's normalizeLabel: [-_] → space, Title Case.
func humanize(name string) string {
	words := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(name))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

func checkQuerySQL(q string) string {
	if q == "" {
		return "querySql is required for query views"
	}
	if len(q) > querySQLMax {
		return "querySql exceeds 20000 characters"
	}
	head := strings.ToUpper(strings.TrimSpace(q))
	if !strings.HasPrefix(head, "SELECT") && !strings.HasPrefix(head, "WITH") {
		return "query must start with SELECT or WITH"
	}
	return ""
}

// checkValidations mirrors the platform's rule checks (types from the
// fixed set, no duplicates, param bounds for min_len/max_len).
func checkValidations(c *defs.Column) error {
	seen := map[string]bool{}
	for _, r := range c.Validations {
		if !validRules[r.Type] {
			return fmt.Errorf("column %s: invalid validation rule %s", c.Name, r.Type)
		}
		if seen[r.Type] {
			return fmt.Errorf("column %s: duplicate validation rule %s", c.Name, r.Type)
		}
		seen[r.Type] = true
		if (r.Type == "min_len" || r.Type == "max_len") && (r.Param < 1 || r.Param > 1000) {
			return fmt.Errorf("column %s: validation rule param must be 1..1000", c.Name)
		}
	}
	return nil
}

// checkFormatting mirrors the platform's formatting-JSON validation.
func checkFormatting(c *defs.Column) error {
	if c.Formatting == "" {
		return nil
	}
	var f struct {
		EnumColors map[string]string `json:"enumColors"`
		Number     *struct {
			Decimals *int `json:"decimals"`
		} `json:"number"`
	}
	if err := json.Unmarshal([]byte(c.Formatting), &f); err != nil {
		return fmt.Errorf("column %s: formatting is not valid JSON", c.Name)
	}
	if len(f.EnumColors) > 0 && c.FieldType != "enum" {
		return fmt.Errorf("column %s: enumColors requires an enum column", c.Name)
	}
	for v, col := range f.EnumColors {
		if !validEnumCols[col] {
			return fmt.Errorf("column %s: unknown enum color %s for value %s", c.Name, col, v)
		}
	}
	if f.Number != nil && c.FieldType != "number" {
		return fmt.Errorf("column %s: number formatting requires a number column", c.Name)
	}
	if f.Number != nil && f.Number.Decimals != nil &&
		(*f.Number.Decimals < 0 || *f.Number.Decimals > 6) {
		return fmt.Errorf("column %s: decimals must be 0..6", c.Name)
	}
	return nil
}

// inspect finds the table's schema and introspected columns. PG tries
// the public schema first, then scans the datasource's schemas; MySQL
// resolves the current database via ListTables (its InspectTable needs
// the schema name).
func (a *App) inspect(table string) (string, []ds.LiveColumn, error) {
	if a.conn.Driver != "mysql" {
		if cols, err := a.a.InspectTable("public", table); err == nil && len(cols) > 0 {
			return "public", cols, nil
		}
	}
	tables, err := a.a.ListTables()
	if err != nil {
		return "", nil, err
	}
	for _, ti := range tables {
		if ti.Name != table {
			continue
		}
		cols, err := a.a.InspectTable(ti.Schema, table)
		if err != nil {
			return "", nil, err
		}
		return ti.Schema, cols, nil
	}
	return "", nil, fmt.Errorf("table %q not found in the datasource", table)
}

// build validates a declaration and produces the frozen-contract table:
// introspection-derived column defaults with overrides merged by name.
func (a *App) build(name string, d Def) (*defs.Table, error) {
	if name == "" {
		return nil, errors.New("def name is required")
	}
	if _, err := ds.QuoteIdent(name); err != nil {
		return nil, fmt.Errorf("def name %q: %v", name, err)
	}
	st := d.SourceType
	if st == "" {
		st = "table"
	}
	if st != "table" && st != "query" {
		return nil, fmt.Errorf("sourceType must be table or query (got %q)", st)
	}
	ps := d.PageSize
	if ps == 0 {
		ps = defaultPageSize
	}
	if ps < 1 || ps > 200 {
		return nil, errors.New("pageSize must be 1..200")
	}

	t := &defs.Table{Name: name, Label: humanize(name), PhysTab: d.Table,
		PageSize: ps, SourceType: st, QuerySQL: d.QuerySQL, Actions: d.Actions}

	var live []ds.LiveColumn
	switch st {
	case "table":
		if d.Table == "" {
			return nil, errors.New("table is required for table defs")
		}
		if _, err := ds.QuoteIdent(d.Table); err != nil {
			return nil, fmt.Errorf("table %q: %v", d.Table, err)
		}
		schema, cols, err := a.inspect(d.Table)
		if err != nil {
			return nil, err
		}
		t.Schema, live = schema, cols
	case "query":
		t.Schema, t.PhysTab = "", ""
		if msg := checkQuerySQL(d.QuerySQL); msg != "" {
			return nil, errors.New(msg)
		}
		if err := a.a.ExplainQuery(d.QuerySQL); err != nil {
			return nil, fmt.Errorf("query failed validation: %v", err)
		}
		cols, _, err := a.a.IntrospectQuery(d.QuerySQL)
		if err != nil {
			return nil, fmt.Errorf("query introspection failed: %v", err)
		}
		live = cols
	}

	// introspection-backed defaults (wizard parity): label humanized,
	// NOT NULL → required (+editable), nullable → editable unless PK,
	// visible/searchable/sortable on.
	t.Columns = make([]defs.Column, len(live))
	for i, lc := range live {
		notNull := !lc.Nullable
		t.Columns[i] = defs.Column{
			Name: lc.Name, Label: humanize(lc.Name), FieldType: lc.FieldType,
			EnumOptions: lc.EnumOptions,
			Editable:    notNull || !lc.IsPK,
			Required:    notNull,
			Visible:     true,
			Searchable:  true,
			Sortable:    true,
			Position:    i,
		}
	}

	// keys: explicit, else the introspected primary key (table defs).
	keys := d.Keys
	if len(keys) == 0 {
		for _, lc := range live {
			if lc.IsPK {
				keys = append(keys, lc.Name)
			}
		}
	}
	byName := map[string]int{}
	for i, c := range t.Columns {
		byName[c.Name] = i
	}
	for _, k := range keys {
		if _, err := ds.QuoteIdent(k); err != nil {
			return nil, fmt.Errorf("key column %q: %v", k, err)
		}
		if _, ok := byName[k]; !ok {
			return nil, fmt.Errorf("key column %q not found on %q", k, d.Table)
		}
	}
	if len(keys) == 0 && st == "table" {
		return nil, fmt.Errorf("table %q has no primary key; set Def.Keys", d.Table)
	}
	t.Keys = keys

	// merge overrides by name (declaration = override, not full listing)
	var virtual []defs.Column
	for _, o := range d.Columns {
		if o.Name == "" {
			return nil, errors.New("override column name is required")
		}
		if o.FK != nil && o.M2M != nil {
			return nil, fmt.Errorf("column %s: an override cannot set both fk and m2m", o.Name)
		}
		if o.M2M != nil {
			if st == "query" {
				return nil, fmt.Errorf("column %s: query views cannot use m2m columns", o.Name)
			}
			if _, exists := byName[o.Name]; exists {
				return nil, fmt.Errorf("column %s: m2m override on a physical column", o.Name)
			}
			if o.M2M.JunctionTable == "" || o.M2M.SrcCol == "" || o.M2M.TgtCol == "" {
				return nil, fmt.Errorf("column %s: m2m needs junctionTable, srcCol and tgtCol", o.Name)
			}
			label := o.Label
			if label == "" {
				label = humanize(o.Name)
			}
			virtual = append(virtual, defs.Column{Name: o.Name, Label: label,
				FieldType: "m2m", M2M: o.M2M, Editable: true, Visible: true,
				Position: len(t.Columns) + len(virtual)})
			continue
		}
		idx, ok := byName[o.Name]
		if !ok {
			return nil, fmt.Errorf("column %q not found on %q (overrides refine introspected columns; only m2m adds virtual ones)",
				o.Name, d.Table)
		}
		c := &t.Columns[idx]
		if o.Label != "" {
			c.Label = o.Label
		}
		if o.Hidden {
			c.Visible = false
		}
		if o.Format != "" {
			c.Formatting = o.Format
			if err := checkFormatting(c); err != nil {
				return nil, err
			}
		}
		if len(o.Validation) > 0 {
			if st == "query" {
				return nil, fmt.Errorf("column %s: query views cannot define validation rules", o.Name)
			}
			c.Validations = o.Validation
			if err := checkValidations(c); err != nil {
				return nil, err
			}
		}
		if o.Editable {
			c.Editable = true
		}
		if o.Required {
			c.Required = true
		}
		if o.Searchable != nil {
			c.Searchable = *o.Searchable
		}
		if o.Sortable != nil {
			c.Sortable = *o.Sortable
		}
		if o.FK != nil {
			if st == "query" {
				return nil, fmt.Errorf("column %s: query views cannot use fk columns", o.Name)
			}
			if o.FK.RefColumn == "" {
				return nil, fmt.Errorf("column %s: fk needs refColumn", o.Name)
			}
			c.BaseType = c.FieldType // fk values validate as their base type
			c.FieldType = "fk"
			c.FK = o.FK
		}
	}
	t.Columns = append(t.Columns, virtual...)

	// default sort: explicit when valid, else engine's first-key fallback
	if d.DefaultSort.Col != "" {
		idx, ok := byName[d.DefaultSort.Col]
		if !ok {
			return nil, fmt.Errorf("default sort column %q not found", d.DefaultSort.Col)
		}
		if !t.Columns[idx].Sortable {
			return nil, fmt.Errorf("default sort column %q is not sortable", d.DefaultSort.Col)
		}
		t.DefaultSortCol = d.DefaultSort.Col
		t.DefaultSortDir = "ASC"
		if d.DefaultSort.Dir == defs.Desc {
			t.DefaultSortDir = "DESC"
		}
	}

	// hooks: map[Event][]string → assignments JSON (order = declaration
	// order), validated by round-tripping through ParseAssignments
	if len(d.Hooks) > 0 {
		asgs := hooks.Assignments{}
		for ev, names := range d.Hooks {
			list := make([]hooks.Assignment, len(names))
			for i, n := range names {
				list[i] = hooks.Assignment{Hook: n, Order: i}
			}
			asgs[ev] = list
		}
		b, err := json.Marshal(asgs)
		if err != nil {
			return nil, err
		}
		if _, err := hooks.ParseAssignments(string(b)); err != nil {
			return nil, err
		}
		t.Hooks = string(b)
	}
	if d.Actions != "" {
		if _, err := hooks.ParseActions(d.Actions); err != nil {
			return nil, err
		}
	}
	return t, nil
}
