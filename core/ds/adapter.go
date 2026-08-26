package ds

import (
	"fmt"
	"strconv"
)

// ListParams carries one page-list request through the Adapter contract.
type ListParams struct {
	Schema, Table    string
	Columns          []string
	Searchable       []string
	Search           string
	SortCol, SortDir string
	Limit, Offset    int
	Filters          []ColumnFilter
}

// FKJoin describes a LEFT JOIN target used to filter an fk column by the
// target table's display value.
type FKJoin struct {
	Schema, Table, RefColumn string
	DisplayColumns           []string
}

// AggregateParams carries one single-value aggregate request (dashboard
// cards). Query set = query-view mode (Schema/Table ignored); otherwise
// the physical table is aggregated. Filters ride the same validated
// pipeline as list requests.
type AggregateParams struct {
	Schema, Table string
	Query         string
	Func          string // count|sum|avg|min|max
	Column        string // required for sum/avg/min/max; empty for count
	Filters       []ColumnFilter
}

// AggregateResult is one aggregate value. Value is nil when the SQL
// aggregate returned NULL (sum/avg/min/max over zero rows); HasRows
// reports whether the filtered set was non-empty (COUNT(*) sidecar).
type AggregateResult struct {
	Value   any
	HasRows bool
}

// scanner is the shared shape of *sql.Row and tx-returned rows.
type scanner interface{ Scan(dest ...any) error }

// scanAggregate scans the (agg, COUNT(*)) pair every aggregate query
// selects; the sidecar count drives HasRows. []byte values (numeric
// strings on some drivers) become plain strings.
func scanAggregate(sc scanner, out *AggregateResult) error {
	var n int64
	if err := sc.Scan(&out.Value, &n); err != nil {
		return err
	}
	out.HasRows = n > 0
	if b, ok := out.Value.([]byte); ok {
		out.Value = string(b)
	}
	return nil
}

// ColumnFilter is one validated per-column filter (AND-combined).
type ColumnFilter struct {
	Column string  // definition column name (validated by the api layer)
	Op     string  // eq|neq|gt|gte|lt|lte|between|in|contains
	Values []any   // coerced: float64 (number), bool (boolean), string otherwise
	Join   *FKJoin // set only for fk display-value filters
}

// TableInfo names one introspectable table.
type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

// LiveColumn is one introspected column of a live table.
type LiveColumn struct {
	Name        string   `json:"name"`
	FieldType   string   `json:"fieldType"`
	Nullable    bool     `json:"nullable"`
	IsPK        bool     `json:"isPk"`
	EnumOptions []string `json:"enumOptions"`
}

// Adapter is the dialect-neutral data access contract. Handlers import
// ONLY this interface — SQL generation and execution live inside adapters.
// A new adapter (SQL or not) implements this and registers in Open;
// nothing else in the codebase changes.
type Adapter interface {
	Ping() error
	ListTables() ([]TableInfo, error)
	InspectTable(schema, table string) ([]LiveColumn, error)

	// Query views: read-only SQL-backed definitions (v1.8). Execution runs
	// inside a read-only transaction with QueryTimeout applied.
	ExplainQuery(query string) error
	IntrospectQuery(query string) ([]LiveColumn, []string, error)
	ListQueryRows(p QueryParams) ([]map[string]any, error)
	CountQueryRows(p QueryParams) (int, error)

	ListRows(p ListParams) ([]map[string]any, error)
	CountRows(p ListParams) (int, error)
	FetchByKey(schema, table string, keyCols []string, keyVals []any, cols []string) ([]map[string]any, error)

	Insert(schema, table string, cols []string, vals []any) error
	UpdateByKey(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (int64, error)
	DeleteByKey(schema, table string, keyCols []string, keyVals []any) (int64, error)

	FetchByRefValues(schema, table, refCol string, displayCols []string, vals []any) (map[string]map[string]any, error)
	CountByRefEq(schema, table, col string, val any) (int, error)

	// Pair is one junction row expressed as (source ref value, target ref
	// value) — the building block of many-to-many relations.
	FetchPairsByRef(schema, table, col, retCol string, vals []any) ([]Pair, error)
	DeletePairs(schema, table, col1 string, val1 any, col2 string, val2 any) (int64, error)

	IsFKViolation(err error) bool
	Close() error
}

// Pair is one junction link (col value → ret value).
type Pair struct {
	Col any `json:"col"`
	Ret any `json:"ret"`
}

// Conn is the dialect-neutral connection description. Raw, when set,
// overrides every other field as a verbatim connection string.
type Conn struct {
	Driver, Host                string
	Port                        int
	DB, User, Password, SSLMode string
	Raw                         string
}

// Open creates the adapter for a connection. Connections are lazy:
// callers open per use and Close when done (brief requirement).
func Open(c Conn) (Adapter, error) {
	switch c.Driver {
	case "", "postgres":
		return openPostgres(c)
	case "mysql":
		return openMySQL(c)
	default:
		return nil, fmt.Errorf("unsupported driver %q", c.Driver)
	}
}

// rowValKeyOf renders a row value as the map key used by rels responses
// (must match api's rowValKey semantics).
func rowValKeyOf(v any) string {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprint(v)
	}
}
