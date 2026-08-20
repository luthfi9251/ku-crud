package ds

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"ku-crud/internal/meta"
)

// MapFieldType maps a PG data_type to a Ku-CRUD field type ("" = excluded).
func MapFieldType(dataType string) string {
	switch dataType {
	case "boolean":
		return "boolean"
	case "smallint", "integer", "bigint", "numeric", "real", "double precision":
		return "number"
	case "timestamp with time zone", "timestamp without time zone",
		"time with time zone", "time without time zone", "date":
		return "datetime"
	case "text", "character varying", "character":
		return "text"
	case "uuid":
		return "uuid"
	case "json", "jsonb":
		return "json"
	}
	return "" // array, bytea, unknown → excluded
}

// parsePGArray parses "{a,b,c}" into []string.
func parsePGArray(s string) []string {
	s = strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

type pgAdapter struct {
	db *sql.DB
}

// pq single-quotes a value per libpq keyword/value rules.
func pq(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func openPostgres(d meta.Datasource) (*pgAdapter, error) {
	conn := d.Raw
	if conn == "" {
		ssl := d.SSLMode
		if ssl == "" {
			ssl = "disable"
		}
		conn = fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
			pq(d.Host), d.Port, pq(d.DBName), pq(d.Username), pq(d.Password), pq(ssl))
	}
	db, err := sql.Open("pgx", conn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &pgAdapter{db: db}, nil
}

func (a *pgAdapter) Ping() error  { return a.db.Ping() }
func (a *pgAdapter) Close() error { return a.db.Close() }

func (a *pgAdapter) IsFKViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23503"
}

// ---- introspection (same SQL as introspect.go) ----

func (a *pgAdapter) ListTables() ([]TableInfo, error) {
	rows, err := a.db.Query(`
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_type='BASE TABLE'
		  AND table_schema NOT IN ('pg_catalog','information_schema')
		ORDER BY table_schema, table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableInfo
	for rows.Next() {
		var ti TableInfo
		if err := rows.Scan(&ti.Schema, &ti.Name); err != nil {
			return nil, err
		}
		out = append(out, ti)
	}
	return out, rows.Err()
}

func (a *pgAdapter) InspectTable(schema, table string) ([]LiveColumn, error) {
	enums, err := a.loadEnums()
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(`
		SELECT c.column_name, c.data_type, c.udt_name, c.is_nullable,
			EXISTS(SELECT 1 FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage k
					ON k.constraint_name = tc.constraint_name AND k.table_schema = tc.table_schema
				WHERE tc.table_schema=$1 AND tc.table_name=$2
				  AND tc.constraint_type='PRIMARY KEY' AND k.column_name = c.column_name)
		FROM information_schema.columns c
		WHERE c.table_schema=$1 AND c.table_name=$2
		ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveColumn
	for rows.Next() {
		var c LiveColumn
		var dataType, udtName, isNullable string
		if err := rows.Scan(&c.Name, &dataType, &udtName, &isNullable, &c.IsPK); err != nil {
			return nil, err
		}
		c.Nullable = isNullable == "YES"
		if dataType == "USER-DEFINED" {
			if opts, ok := enums[udtName]; ok {
				c.FieldType = "enum"
				c.EnumOptions = opts
				out = append(out, c)
			}
			continue
		}
		c.FieldType = MapFieldType(dataType)
		if c.FieldType == "" {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *pgAdapter) loadEnums() (map[string][]string, error) {
	rows, err := a.db.Query(`
		SELECT t.typname, array_agg(e.enumlabel ORDER BY e.enumsortorder)
		FROM pg_type t
		JOIN pg_enum e ON e.enumtypid = t.oid
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname NOT IN ('pg_catalog','information_schema')
		GROUP BY t.typname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	enums := map[string][]string{}
	for rows.Next() {
		var name string
		var opts any
		if err := rows.Scan(&name, &opts); err != nil {
			return nil, err
		}
		var s string
		switch v := opts.(type) {
		case []byte:
			s = string(v)
		case string:
			s = v
		}
		enums[name] = parsePGArray(s)
	}
	return enums, rows.Err()
}

// ---- data access via sqlkit ----

func (a *pgAdapter) ListRows(p ListParams) ([]map[string]any, error) {
	sqlText, args, err := pgDialect.buildList(p)
	if err != nil {
		return nil, err
	}
	return a.queryMaps(sqlText, args...)
}

func (a *pgAdapter) CountRows(p ListParams) (int, error) {
	sqlText, args, err := pgDialect.buildCount(p)
	if err != nil {
		return 0, err
	}
	var n int
	if err := a.db.QueryRow(sqlText, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (a *pgAdapter) FetchByKey(schema, table string, keyCols []string, keyVals []any, cols []string) ([]map[string]any, error) {
	sqlText, args, err := pgDialect.buildFetchByKey(schema, table, keyCols, keyVals, cols)
	if err != nil {
		return nil, err
	}
	return a.queryMaps(sqlText, args...)
}

func (a *pgAdapter) Insert(schema, table string, cols []string, vals []any) error {
	sqlText, err := pgDialect.buildInsert(schema, table, cols, vals)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(sqlText, vals...)
	return err
}

func (a *pgAdapter) UpdateByKey(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (int64, error) {
	sqlText, args, err := pgDialect.buildUpdateByKey(schema, table, setCols, setVals, keyCols, keyVals)
	if err != nil {
		return 0, err
	}
	res, err := a.db.Exec(sqlText, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (a *pgAdapter) DeleteByKey(schema, table string, keyCols []string, keyVals []any) (int64, error) {
	sqlText, args, err := pgDialect.buildDeleteByKey(schema, table, keyCols, keyVals)
	if err != nil {
		return 0, err
	}
	res, err := a.db.Exec(sqlText, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (a *pgAdapter) FetchByRefValues(schema, table, refCol string, displayCols []string, vals []any) (map[string]map[string]any, error) {
	sqlText, args, err := pgDialect.buildFetchByRefValues(schema, table, refCol, displayCols, vals)
	if err != nil {
		return nil, err
	}
	maps, err := a.queryMaps(sqlText, args...)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, m := range maps {
		out[rowValKeyOf(m[refCol])] = m
	}
	return out, nil
}

func (a *pgAdapter) CountByRefEq(schema, table, col string, val any) (int, error) {
	sqlText, args, err := pgDialect.buildCountByRefEq(schema, table, col, val)
	if err != nil {
		return 0, err
	}
	var n int
	if err := a.db.QueryRow(sqlText, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (a *pgAdapter) FetchPairsByRef(schema, table, col, retCol string, vals []any) ([]Pair, error) {
	sqlText, args, err := pgDialect.buildFetchPairs(schema, table, col, retCol, vals)
	if err != nil {
		return nil, err
	}
	rows, err := a.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pair
	for rows.Next() {
		var c, r any
		if err := rows.Scan(&c, &r); err != nil {
			return nil, err
		}
		if b, ok := c.([]byte); ok {
			c = string(b)
		}
		if b, ok := r.([]byte); ok {
			r = string(b)
		}
		out = append(out, Pair{Col: c, Ret: r})
	}
	return out, rows.Err()
}

func (a *pgAdapter) DeletePairs(schema, table, col1 string, val1 any, col2 string, val2 any) (int64, error) {
	sqlText, _, err := pgDialect.buildDeletePairs(schema, table, col1, col2)
	if err != nil {
		return 0, err
	}
	res, err := a.db.Exec(sqlText, val1, val2)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (a *pgAdapter) queryMaps(sqlText string, args ...any) ([]map[string]any, error) {
	rows, err := a.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		scan := scanTargets(len(cols))
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		out = append(out, rowToMap(cols, deref(scan)))
	}
	return out, rows.Err()
}
