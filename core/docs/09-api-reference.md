# 09 — API reference

Compact per-package reference of every exported identifier. Signatures are
authoritative; semantics link to the deep-dive pages.

## Package `kucrud` (root)

*Re-exports for ergonomics:* `type Conn = ds.Conn`, `type FK = defs.FK`,
`type M2M = defs.M2M`, `type SortDir = defs.SortDir`,
`type ValidationRule = defs.ValidationRule`, `type Event = hooks.Event`,
`type HookFunc = hooks.HookFunc`, `type RowPayload = hooks.RowPayload`,
`type HookContext = hooks.HookContext`, `type Op = httpapi.Op`,
`type Gate = httpapi.Gate`; constants `Asc`, `Desc`,
`BeforeCreate/AfterCreate/BeforeUpdate/AfterUpdate/BeforeDelete/AfterDelete`,
`OpRead/OpCreate/OpUpdate/OpDelete/OpAction/OpExport/OpImport`.

```go
type SortSpec struct { Col string; Dir defs.SortDir }
func Sort(col string, dir defs.SortDir) SortSpec

type Def struct {
    Table       string
    Keys        []string
    Columns     []Override
    DefaultSort SortSpec
    Hooks       map[Event][]string
    Actions     string
    SourceType  string // "table" (default) | "query"
    QuerySQL    string
    PageSize    int    // 1..200, default 20
}

type Override struct {
    Name       string
    Label      string
    Hidden     bool
    Format     string
    Validation []defs.ValidationRule
    FK         *defs.FK
    M2M        *defs.M2M
    Editable   bool
    Required   bool
    Searchable *bool
    Sortable   *bool
}

type App struct{ /* … */ }
type Option func(*App)

func WithGate(g Gate) Option
func WithHookRegistry(r *hooks.Registry) Option

func New(c Conn, opts ...Option) (*App, error)
func (a *App) Close() error
func (a *App) Resource(name string, d Def) (http.Handler, error)
func (a *App) CRUD(path string, d Def) *App          // panics on error; chainable
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

→ [02 — Getting started](02-getting-started.md), [03 — Definitions](03-definitions.md).

## Package `defs`

```go
const MissingTable = "\x00missing" // dangling FK/M2M target; "" = self-reference

type SortDir string
const ( Asc SortDir = "asc"; Desc SortDir = "desc" )

type Table struct {
    Name, Label, Description                    string
    Schema, PhysTab                             string
    Keys                                        []string
    PageSize                                    int
    DefaultSortCol, DefaultSortDir              string // "ASC"/"DESC"
    DefaultView, ViewConfig                     string
    SourceType, QuerySQL                        string // "table" | "query"
    Hooks, Actions                              string // assignment/action JSON
    Columns                                     []Column
}

type Column struct {
    Name, Label, FieldType                      string
    EnumOptions                                 []string
    Editable, Required, Visible, Searchable, Sortable bool
    Position                                    int
    Validations                                 []ValidationRule
    Formatting                                  string
    IsComputed                                  bool
    ComputedFormula                             string
    BaseType                                    string // fk: underlying introspected type
    FK                                          *FK
    M2M                                         *M2M
}

type FK struct { Table, RefColumn string; DisplayColumns []string }
type M2M struct { JunctionTable, SrcCol, TgtCol string; DisplayColumns []string }
type ValidationRule struct { Type string; Param int } // email|min_len|max_len|number|text
```

## Package `ds`

```go
type Conn struct { Driver, Host string; Port int; DB, User, Password, SSLMode, Raw string }
func Open(c Conn) (Adapter, error)
func QuoteIdent(s string) (string, error)     // identifier allowlist ^[A-Za-z_][A-Za-z0-9_]*$
func MapFieldType(dataType string) string     // "" = excluded
var QueryTimeout time.Duration                // default 15s (query views)
func IsQueryTimeout(err error) bool           // PG 57014; MySQL 3024/1969

type Adapter interface { /* 23 methods — see 07 */ }
type ListParams struct{ … }  type QueryParams struct{ … }
type AggregateParams struct{ … }  type AggregateResult struct{ Value any; HasRows bool }
type ColumnFilter struct{ Column, Op string; Values []any; Join *FKJoin }
type FKJoin struct{ Schema, Table, RefColumn string; DisplayColumns []string }
type TableInfo struct{ Schema, Name string }
type LiveColumn struct{ Name, FieldType string; Nullable, IsPK bool; EnumOptions []string }
type Pair struct{ Col, Ret any }

type DriftReport struct{ Missing, Added, TypeChanged []string }
func (r DriftReport) Empty() bool
func EffectiveType(c defs.Column) string
func CompareDrift(defined []defs.Column, live []LiveColumn) DriftReport
```

→ [07 — Datasources](07-datasources.md).

## Package `engine`

```go
type Resolver interface {
    Adapter(t *defs.Table) (ds.Adapter, error)
    Resolve(name string) (*defs.Table, error)
}
var ErrDSNotFound = errors.New("datasource not found") // → 404
var ErrConn       = errors.New("connection failed")    // → 502

type ReadService struct {
    R       Resolver
    FKJoin  FKJoinResolver      // nil rejects fk filter columns
    CanRead func(name string) bool // nil allows every target
}
func (s *ReadService) List(w, r, t);  Get;  ExportCSV;  Stats;  FKOptions;  M2MOptions;  M2MLinks

type Hooks interface {
    Guard(t *defs.Table) error
    RunBefore(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error)
    RunAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload) error
}
type SyncAfterHooks interface {
    Hooks
    RunSyncAfter(ev hooks.Event, t *defs.Table, row hooks.RowPayload, rowKey string)
}

type RefSource struct{ Src *defs.Table; Column, RefColumn, Label string }
type WriteService struct {
    R          Resolver
    H          Hooks // may be nil
    CanWrite   func(table, grant string) bool          // junction writes; nil allows all
    RefSources func(t *defs.Table) ([]RefSource, error) // delete protection; nil = none
}
func (s *WriteService) Insert(w, r, t);  Update;  Delete;  BulkDelete

type ImportService struct { R Resolver; H Hooks }
func (s *ImportService) PreviewImport(w, r, t);  ApplyImport
const ImportMaxFile = 5 << 20

type FKJoinResolver func(column string) (*ds.FKJoin, error)
func ParseFilters(t *defs.Table, raw string, fkJoin FKJoinResolver) ([]ds.ColumnFilter, error)
func ResolveSort(t *defs.Table, sortCol, sortDir string) (string, string)
func ValidateColumn(c defs.Column, v any) error
func EditablePayload(t *defs.Table, body map[string]any, isInsert bool) ([]string, []any, error)
func ApplyComputed(cols []defs.Column, rows []map[string]any)
func CompileComputed(c defs.Column, cols []defs.Column) (string, func(map[string]any) any, error)

func EncodeRowKey(vals []string) string                    // base64url(JSON string array)
func DecodeKey(t *defs.Table, raw string) ([]any, error)   // coerced to column types
func EncodeKey(t *defs.Table, vals map[string]any) (string, error) // audit JSON form

type M2MCfg struct {
    Junction, Target *defs.Table
    SrcCol, TgtCol, SrcRef, TargetRef string
    TargetMissing bool
}
func ResolveM2M(r Resolver, t *defs.Table, c defs.Column) (*M2MCfg, string)

// csvutil (sub-package engine/csvutil)
func ParseCSV(data []byte) (rune, []string, [][]string, error)
func AutoMap(headers []string, cols []defs.Column) map[string]string
func CoerceValidate(c defs.Column, raw string) (any, error)
```

→ [08 — Embedding the engine](08-embedding.md).

## Package `hooks`

```go
type Event string
const ( BeforeCreate, AfterCreate, BeforeUpdate, AfterUpdate, BeforeDelete, AfterDelete, OnAction Event )

type RowPayload struct{ Values, Old map[string]any; Message string }
type HookContext struct {
    Actor string; Table *defs.Table; Columns []defs.Column
    Open func(name string) (ds.Adapter, error); Logger *slog.Logger; Host any
}
type HookFunc func(ctx context.Context, hc *HookContext, ev Event,
    row RowPayload, cfg json.RawMessage) (RowPayload, error)

func WithActor(ctx context.Context, name string) context.Context
func ActorFrom(ctx context.Context) string

var Default = NewRegistry()
func Register(name string, fn HookFunc) error
type Registry struct{ /* … */ }
func NewRegistry() *Registry
func (r *Registry) Register(name string, fn HookFunc) error
func (r *Registry) Get(name string) (HookFunc, bool)
func (r *Registry) Names() []string

type Assignment struct{ Hook string; Config json.RawMessage; Order int }
type Assignments map[Event][]Assignment
func ParseAssignments(s string) (Assignments, error)
func (a Assignments) Names() []string
type MissingError struct{ Name string }
func (r *Registry) CheckMissing(asgs Assignments) error

const ( BeforeTimeout = 5*time.Second; AfterTimeout = 30*time.Second; ActionTimeout = 15*time.Second )
func (r *Registry) RunBefore(ctx, ev, asgs, hc, row) (RowPayload, error)
func (r *Registry) RunOne(ctx, hc, ev, row, a Assignment) error
func (r *Registry) RunAction(ctx, hc, row, a Assignment) (string, error)

type CustomAction struct{ ID, Label, Confirm, Grant, Hook string; Config json.RawMessage; Order int; Style string }
type ActionsConfig struct{ Hidden []string; Custom []CustomAction }
func (c ActionsConfig) Find(id string) *CustomAction
func ParseActions(s string) (ActionsConfig, error)
```

→ [06 — Hooks & actions](06-hooks-and-actions.md).

## Package `httpapi`

```go
type Op string // read | create | update | delete | action | export | import
type Gate func(r *http.Request, op Op, table string) error

type DefSource interface {
    Resolve(name string) (*defs.Table, error)
    Adapter(t *defs.Table) (ds.Adapter, error)
    Defs() []*defs.Table
}

type ServiceSet struct {
    Read   *engine.ReadService
    Write  *engine.WriteService
    Import *engine.ImportService
}
type Options struct {
    Gate     Gate
    Registry *hooks.Registry // nil → hooks.Default
    Services func(r *http.Request, t *defs.Table) ServiceSet // nil → DefSource defaults
}

type Resource struct{ /* … */ }
func New(name string, t *defs.Table, src DefSource, o Options) *Resource
func (h *Resource) ServeHTTP(w http.ResponseWriter, r *http.Request)
func DefsHandler(src DefSource, gate Gate) http.Handler // GET /api/defs
```

DTOs (`DefDTO`, `ColumnDTO`, `PermsDTO`, `FKDTO`, `M2MDTO`) are documented in
[04 — HTTP API](04-http-api.md).

→ [04](04-http-api.md), [05 — Authorization](05-authorization.md).
