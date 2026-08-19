package ds

import (
	"database/sql"
	"strings"
)

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
	}
	return "" // array, json, uuid, bytea, unknown → excluded in v1
}

type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

type LiveColumn struct {
	Name        string   `json:"name"`
	FieldType   string   `json:"fieldType"`
	Nullable    bool     `json:"nullable"`
	IsPK        bool     `json:"isPk"`
	EnumOptions []string `json:"enumOptions"`
}

func ListTables(db *sql.DB) ([]TableInfo, error) {
	rows, err := db.Query(`
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

func InspectTable(db *sql.DB, schema, table string) ([]LiveColumn, error) {
	enums, err := loadEnums(db)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
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
			continue // non-enum user types excluded
		}
		c.FieldType = MapFieldType(dataType)
		if c.FieldType == "" {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func loadEnums(db *sql.DB) (map[string][]string, error) {
	rows, err := db.Query(`
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
		var opts any // pg array literal, scanned as []byte
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

// parsePGArray parses "{a,b,c}" into []string.
func parsePGArray(s string) []string {
	s = strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
