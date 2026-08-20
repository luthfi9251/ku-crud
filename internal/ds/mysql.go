package ds

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"ku-crud/internal/meta"
)

type mysqlAdapter struct {
	db *sql.DB
}

func openMySQL(d meta.Datasource) (*mysqlAdapter, error) {
	var dsn string
	if d.Raw != "" {
		dsn = d.Raw
	} else {
		cfg := mysql.NewConfig()
		cfg.User = d.Username
		cfg.Passwd = d.Password
		cfg.Net = "tcp"
		cfg.Addr = fmt.Sprintf("%s:%d", d.Host, d.Port)
		cfg.DBName = d.DBName
		cfg.ParseTime = true
		cfg.Params = map[string]string{"charset": "utf8mb4"}
		if d.SSLMode != "" && d.SSLMode != "disable" {
			cfg.TLSConfig = "true"
		}
		dsn = cfg.FormatDSN()
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &mysqlAdapter{db: db}, nil
}

func (a *mysqlAdapter) Ping() error  { return a.db.Ping() }
func (a *mysqlAdapter) Close() error { return a.db.Close() }

func (a *mysqlAdapter) IsFKViolation(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1451 || me.Number == 1452)
}

// mapMySQLType maps information_schema columns to Ku-CRUD field types.
func mapMySQLType(dataType, columnType string) (string, []string) {
	switch dataType {
	case "tinyint":
		if strings.HasPrefix(columnType, "tinyint(1)") {
			return "boolean", nil
		}
		return "number", nil
	case "smallint", "mediumint", "int", "bigint", "decimal", "float", "double":
		return "number", nil
	case "date", "datetime", "timestamp", "time":
		return "datetime", nil
	case "char", "varchar", "text", "tinytext", "mediumtext", "longtext":
		return "text", nil
	case "enum":
		return "enum", parseMysqlEnum(columnType)
	case "json":
		return "json", nil
	}
	return "", nil // blob, geometry, etc. excluded
}

func parseMysqlEnum(columnType string) []string {
	s := strings.TrimSuffix(strings.TrimPrefix(columnType, "enum("), ")")
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inQuote:
			inQuote = true
		case c == '\'' && inQuote:
			if i+1 < len(s) && s[i+1] == '\'' { // '' escape → literal '
				cur.WriteByte('\'')
				i++
			} else {
				inQuote = false
			}
		case c == ',' && !inQuote:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	if inQuote {
		return nil // unterminated quote — unparseable
	}
	flush()
	return out
}

func (a *mysqlAdapter) ListTables() ([]TableInfo, error) {
	rows, err := a.db.Query(`
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_type='BASE TABLE' AND table_schema=DATABASE()
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

func (a *mysqlAdapter) InspectTable(schema, table string) ([]LiveColumn, error) {
	rows, err := a.db.Query(`
		SELECT c.column_name, c.data_type, c.column_type, c.is_nullable,
			EXISTS(SELECT 1 FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage k
					ON k.constraint_name=tc.constraint_name AND k.table_schema=tc.table_schema
				WHERE tc.table_schema=? AND tc.table_name=?
				  AND tc.constraint_type='PRIMARY KEY' AND k.column_name=c.column_name)
		FROM information_schema.columns c
		WHERE c.table_schema=? AND c.table_name=?
		ORDER BY c.ordinal_position`, schema, table, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveColumn
	for rows.Next() {
		var c LiveColumn
		var dataType, columnType, isNullable string
		if err := rows.Scan(&c.Name, &dataType, &columnType, &isNullable, &c.IsPK); err != nil {
			return nil, err
		}
		c.Nullable = isNullable == "YES"
		c.FieldType, c.EnumOptions = mapMySQLType(dataType, columnType)
		if c.FieldType == "" {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- data access: identical shape to postgres.go but on mysqlDialect ----

// boolCols returns the set of tinyint(1) column names for a table (empty on error).
func (a *mysqlAdapter) boolCols(schema, table string) map[string]bool {
	cols, err := a.InspectTable(schema, table)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, c := range cols {
		if c.FieldType == "boolean" {
			set[c.Name] = true
		}
	}
	return set
}

// coerceBools converts int64 0/1 to bool for the given boolean columns.
func coerceBools(rows []map[string]any, bools map[string]bool) {
	if len(bools) == 0 {
		return
	}
	for _, m := range rows {
		for name := range bools {
			if v, ok := m[name]; ok {
				if i, ok := v.(int64); ok && (i == 0 || i == 1) {
					m[name] = i == 1
				}
			}
		}
	}
}

func (a *mysqlAdapter) ListRows(p ListParams) ([]map[string]any, error) {
	sqlText, args, err := mysqlDialect.buildList(p)
	if err != nil {
		return nil, err
	}
	rows, err := a.queryMaps(sqlText, args...)
	if err != nil {
		return nil, err
	}
	coerceBools(rows, a.boolCols(p.Schema, p.Table))
	return rows, nil
}

func (a *mysqlAdapter) CountRows(p ListParams) (int, error) {
	sqlText, args, err := mysqlDialect.buildCount(p)
	if err != nil {
		return 0, err
	}
	var n int
	if err := a.db.QueryRow(sqlText, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (a *mysqlAdapter) FetchByKey(schema, table string, keyCols []string, keyVals []any, cols []string) ([]map[string]any, error) {
	sqlText, args, err := mysqlDialect.buildFetchByKey(schema, table, keyCols, keyVals, cols)
	if err != nil {
		return nil, err
	}
	rows, err := a.queryMaps(sqlText, args...)
	if err != nil {
		return nil, err
	}
	coerceBools(rows, a.boolCols(schema, table))
	return rows, nil
}

func (a *mysqlAdapter) Insert(schema, table string, cols []string, vals []any) error {
	sqlText, err := mysqlDialect.buildInsert(schema, table, cols, vals)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(sqlText, vals...)
	return err
}

func (a *mysqlAdapter) UpdateByKey(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (int64, error) {
	sqlText, args, err := mysqlDialect.buildUpdateByKey(schema, table, setCols, setVals, keyCols, keyVals)
	if err != nil {
		return 0, err
	}
	res, err := a.db.Exec(sqlText, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (a *mysqlAdapter) DeleteByKey(schema, table string, keyCols []string, keyVals []any) (int64, error) {
	sqlText, args, err := mysqlDialect.buildDeleteByKey(schema, table, keyCols, keyVals)
	if err != nil {
		return 0, err
	}
	res, err := a.db.Exec(sqlText, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (a *mysqlAdapter) FetchByRefValues(schema, table, refCol string, displayCols []string, vals []any) (map[string]map[string]any, error) {
	sqlText, args, err := mysqlDialect.buildFetchByRefValues(schema, table, refCol, displayCols, vals)
	if err != nil {
		return nil, err
	}
	maps, err := a.queryMaps(sqlText, args...)
	if err != nil {
		return nil, err
	}
	coerceBools(maps, a.boolCols(schema, table))
	out := map[string]map[string]any{}
	for _, m := range maps {
		out[rowValKeyOf(m[refCol])] = m
	}
	return out, nil
}

func (a *mysqlAdapter) CountByRefEq(schema, table, col string, val any) (int, error) {
	sqlText, args, err := mysqlDialect.buildCountByRefEq(schema, table, col, val)
	if err != nil {
		return 0, err
	}
	var n int
	if err := a.db.QueryRow(sqlText, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (a *mysqlAdapter) queryMaps(sqlText string, args ...any) ([]map[string]any, error) {
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
