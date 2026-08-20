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
		{"json", "json", "", nil},
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
		`CREATE TABLE people(id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(80) NOT NULL, balance DECIMAL(10,2))`,
		`INSERT INTO people(name,balance) VALUES ('jo',10.5),('joe',2),('extra',NULL)`,
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
	if err != nil || len(cols) != 3 || !cols[0].IsPK || cols[0].FieldType != "number" || cols[1].FieldType != "text" {
		t.Fatalf("inspect: %v %+v", err, cols)
	}

	lp := ListParams{Schema: schema, Table: "people", Columns: []string{"id", "name"},
		Searchable: []string{"name"}, Search: "jo", SortCol: "id", SortDir: "ASC", Limit: 10, Offset: 0}
	rows, err := a.ListRows(lp)
	if err != nil || len(rows) != 2 {
		t.Fatalf("listRows: %v %+v", err, rows)
	}
	if total, err := a.CountRows(lp); err != nil || total != 2 {
		t.Fatalf("countRows: %v %d", err, total)
	}
	got, err := a.FetchByKey(schema, "people", []string{"id"}, []any{1}, []string{"id", "name"})
	if err != nil || len(got) != 1 || got[0]["name"] != "jo" {
		t.Fatalf("fetchByKey: %v %+v", err, got)
	}
	if err := a.Insert(schema, "people", []string{"name", "balance"}, []any{"zoe", 9}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := a.UpdateByKey(schema, "people", []string{"name"}, []any{"zoe2"}, []string{"name"}, []any{"zoe"})
	if err != nil || n != 1 {
		t.Fatalf("update: %v %d", err, n)
	}
	rel, err := a.FetchByRefValues(schema, "people", "name", []string{"id"}, []any{"jo", "joe"})
	if err != nil || len(rel) != 2 || rel["jo"]["name"] != "jo" {
		t.Fatalf("refValues: %v %+v", err, rel)
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
