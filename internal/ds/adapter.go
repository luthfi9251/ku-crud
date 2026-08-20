package ds

import (
	"fmt"
	"strconv"

	"ku-crud/internal/meta"
)

// Adapter is the dialect-neutral data access contract. Handlers import
// ONLY this interface — SQL generation and execution live inside adapters.
// A new adapter (SQL or not) implements this and registers in Open;
// nothing else in the codebase changes.
type Adapter interface {
	Ping() error
	ListTables() ([]TableInfo, error)
	InspectTable(schema, table string) ([]LiveColumn, error)

	ListRows(p ListParams) ([]map[string]any, error)
	CountRows(p ListParams) (int, error)
	FetchByKey(schema, table string, keyCols []string, keyVals []any, cols []string) ([]map[string]any, error)

	Insert(schema, table string, cols []string, vals []any) error
	UpdateByKey(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (int64, error)
	DeleteByKey(schema, table string, keyCols []string, keyVals []any) (int64, error)

	FetchByRefValues(schema, table, refCol string, displayCols []string, vals []any) (map[string]map[string]any, error)
	CountByRefEq(schema, table, col string, val any) (int, error)

	IsFKViolation(err error) bool
	Close() error
}

// Open creates the adapter for a stored datasource. Connections are lazy:
// callers open per use and Close when done (brief requirement).
func Open(d meta.Datasource) (Adapter, error) {
	switch d.Driver {
	case "", "postgres":
		return openPostgres(d)
	case "mysql":
		return openMySQL(d)
	default:
		return nil, fmt.Errorf("unsupported driver %q", d.Driver)
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

// openMySQL arrives in Task 5.
func openMySQL(d meta.Datasource) (Adapter, error) {
	return nil, fmt.Errorf("mysql adapter not yet implemented")
}
