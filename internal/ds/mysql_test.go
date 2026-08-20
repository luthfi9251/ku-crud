package ds

import (
	"reflect"
	"testing"
)

func TestParseMysqlEnum(t *testing.T) {
	got := parseMysqlEnum("enum('sunny','rainy')")
	if !reflect.DeepEqual(got, []string{"sunny", "rainy"}) {
		t.Fatalf("enum: %v", got)
	}
	if parseMysqlEnum("enum('a')") == nil || len(parseMysqlEnum("enum('a')")) != 1 {
		t.Fatal("single enum")
	}
	if got := parseMysqlEnum("enum('a,b','c')"); !reflect.DeepEqual(got, []string{"a,b", "c"}) {
		t.Fatalf("comma literal: %v", got)
	}
	if got := parseMysqlEnum("enum('it''s','x')"); !reflect.DeepEqual(got, []string{"it's", "x"}) {
		t.Fatalf("escaped quote: %v", got)
	}
	if got := parseMysqlEnum("enum('unterminated"); got != nil {
		t.Fatalf("unterminated: %v", got)
	}
}

func TestMapMySQLType(t *testing.T) {
	cases := []struct {
		dataType, columnType, want string
		opts                       []string
	}{
		{"tinyint", "tinyint(1)", "boolean", nil},
		{"tinyint", "tinyint(4)", "number", nil},
		{"int", "int(11)", "number", nil},
		{"decimal", "decimal(10,2)", "number", nil},
		{"double", "double", "number", nil},
		{"date", "date", "datetime", nil},
		{"datetime", "datetime", "datetime", nil},
		{"timestamp", "timestamp", "datetime", nil},
		{"varchar", "varchar(80)", "text", nil},
		{"text", "text", "text", nil},
		{"enum", "enum('a','b')", "enum", []string{"a", "b"}},
		{"json", "json", "json", nil},
		{"blob", "blob", "", nil},
	}
	for _, c := range cases {
		ft, opts := mapMySQLType(c.dataType, c.columnType)
		if ft != c.want || !reflect.DeepEqual(opts, c.opts) {
			t.Fatalf("%s/%s: got %q %v want %q %v", c.dataType, c.columnType, ft, opts, c.want, c.opts)
		}
	}
}

func TestMySQLAdapterCRUD(t *testing.T) {
	db := mysqlTestDB(t)
	var schema string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatalf("select database: %v", err)
	}
	// DDL split into single statements: the mysql driver rejects
	// multi-statement Exec unless the DSN enables multiStatements.
	// child is dropped first so re-runs don't fail on the people FK.
	ddl := []string{
		`DROP TABLE IF EXISTS child`,
		`DROP TABLE IF EXISTS people`,
		`CREATE TABLE people(id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(80) NOT NULL, balance DECIMAL(10,2), active TINYINT(1) NOT NULL DEFAULT 0)`,
		`INSERT INTO people(name,balance,active) VALUES ('jo',10.5,1),('joe',2,0),('extra',NULL,0)`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Skipf("mysql ddl: %v", err)
		}
	}
	a := &mysqlAdapter{db: db}
	defer a.Close()

	tables, err := a.ListTables()
	if err != nil || len(tables) == 0 {
		t.Fatalf("listTables: %v %+v", err, tables)
	}
	found := false
	for _, ti := range tables {
		if ti.Schema == schema && ti.Name == "people" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s.people not listed: %+v", schema, tables)
	}
	cols, err := a.InspectTable(schema, "people")
	if err != nil || len(cols) != 4 || !cols[0].IsPK || cols[0].FieldType != "number" || cols[1].FieldType != "text" {
		t.Fatalf("inspect: %v %+v", err, cols)
	}
	if cols[3].FieldType != "boolean" {
		t.Fatalf("active not boolean: %+v", cols[3])
	}

	lp := ListParams{Schema: schema, Table: "people", Columns: []string{"id", "name", "active"},
		Searchable: []string{"name"}, Search: "jo", SortCol: "id", SortDir: "ASC", Limit: 10, Offset: 0}
	rows, err := a.ListRows(lp)
	if err != nil || len(rows) != 2 {
		t.Fatalf("listRows: %v %+v", err, rows)
	}
	if rows[0]["active"] != true {
		t.Fatalf("listRows active not bool true: %T %v", rows[0]["active"], rows[0]["active"])
	}
	if total, err := a.CountRows(lp); err != nil || total != 2 {
		t.Fatalf("countRows: %v %d", err, total)
	}
	got, err := a.FetchByKey(schema, "people", []string{"id"}, []any{1}, []string{"id", "name", "active"})
	if err != nil || len(got) != 1 || got[0]["name"] != "jo" {
		t.Fatalf("fetchByKey: %v %+v", err, got)
	}
	if got[0]["active"] != true {
		t.Fatalf("fetchByKey active not bool true: %T %v", got[0]["active"], got[0]["active"])
	}
	if err := a.Insert(schema, "people", []string{"name", "balance"}, []any{"zoe", 9}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := a.UpdateByKey(schema, "people", []string{"name"}, []any{"zoe2"}, []string{"name"}, []any{"zoe"})
	if err != nil || n != 1 {
		t.Fatalf("update: %v %d", err, n)
	}
	if n, err = a.UpdateByKey(schema, "people", []string{"active"}, []any{false}, []string{"id"}, []any{1}); err != nil || n != 1 {
		t.Fatalf("update active: %v %d", err, n)
	}
	got, err = a.FetchByKey(schema, "people", []string{"id"}, []any{1}, []string{"active"})
	if err != nil || len(got) != 1 || got[0]["active"] != false {
		t.Fatalf("fetchByKey active after update: %v %+v", err, got)
	}
	rel, err := a.FetchByRefValues(schema, "people", "name", []string{"id", "active"}, []any{"jo", "joe"})
	if err != nil || len(rel) != 2 || rel["jo"]["name"] != "jo" {
		t.Fatalf("refValues: %v %+v", err, rel)
	}
	if rel["jo"]["active"] != false || rel["joe"]["active"] != false {
		t.Fatalf("refValues active not bool: %+v", rel)
	}
	if cnt, err := a.CountByRefEq(schema, "people", "name", "joe"); err != nil || cnt != 1 {
		t.Fatalf("countByRefEq: %v %d", err, cnt)
	}
	if n, err = a.DeleteByKey(schema, "people", []string{"name"}, []any{"zoe2"}); err != nil || n != 1 {
		t.Fatalf("delete: %v %d", err, n)
	}
	// FK violation mapping: 1451 (child rows exist)
	fkDDL := []string{
		`CREATE TABLE child(id INT AUTO_INCREMENT PRIMARY KEY, pid INT, FOREIGN KEY (pid) REFERENCES people(id))`,
		`INSERT INTO child(pid) VALUES (1)`,
	}
	for _, stmt := range fkDDL {
		if _, err := db.Exec(stmt); err != nil {
			t.Skipf("mysql fk ddl: %v", err)
		}
	}
	_, err = a.DeleteByKey(schema, "people", []string{"id"}, []any{1})
	if err == nil || !a.IsFKViolation(err) {
		t.Fatalf("fk violation not detected: %v", err)
	}
}
