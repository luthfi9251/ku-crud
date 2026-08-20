package meta

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// buildV12DB simulates an existing v1.2 instance: migrations 1-5 applied with
// one table def + column.
func buildV12DB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v12.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES('schema_version','5')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode,raw,driver)
		VALUES('d','h',1,'db','u','p','disable','','postgres')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,key_columns,page_size)
		VALUES(1,'public','t','T','["id"]',20)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO columns(table_def_id,name,label,field_type,enum_options,position)
		VALUES(1,'id','ID','number',NULL,0)`); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigration6Upgrade(t *testing.T) {
	path := buildV12DB(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='schema_version'`).Scan(&v); err != nil || v != "6" {
		t.Fatalf("schema_version=%q err=%v", v, err)
	}
	// new table_defs columns exist with defaults
	var col, dir string
	if err := s.db.QueryRow(`SELECT default_sort_col, default_sort_dir FROM table_defs WHERE id=1`).Scan(&col, &dir); err != nil {
		t.Fatalf("default sort cols: %v", err)
	}
	if col != "" || dir != "ASC" {
		t.Fatalf("legacy defaults: col=%q dir=%q", col, dir)
	}
	// new columns-table m2m columns exist with defaults
	var jid int64
	var src, tgt, disp string
	if err := s.db.QueryRow(`SELECT m2m_junction_def_id, m2m_junction_src_col, m2m_junction_tgt_col, m2m_display_cols
		FROM columns WHERE id=1`).Scan(&jid, &src, &tgt, &disp); err != nil {
		t.Fatalf("m2m cols: %v", err)
	}
	if jid != 0 || src != "" || tgt != "" || disp != "" {
		t.Fatalf("m2m defaults: %d %q %q %q", jid, src, tgt, disp)
	}
}

func TestTableDefDefaultSortRoundtrip(t *testing.T) {
	s := openTest(t)
	defer s.Close()
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db", Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t", Label: "T",
		KeyColumns: []string{"id"}, PageSize: 20,
		DefaultSortCol: "name", DefaultSortDir: "DESC"}
	cols := []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Required: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Sortable: true, Position: 1},
	}
	if err := s.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultSortCol != "name" || got.DefaultSortDir != "DESC" {
		t.Fatalf("roundtrip: %+v", got)
	}
	got.DefaultSortCol, got.DefaultSortDir = "", "ASC"
	if err := s.UpdateTableDef(got, cols); err != nil {
		t.Fatal(err)
	}
	got2, _, _ := s.GetTableDef(def.ID)
	if got2.DefaultSortCol != "" || got2.DefaultSortDir != "ASC" {
		t.Fatalf("clear roundtrip: %+v", got2)
	}
	// list path carries the fields too
	list, err := s.ListTableDefs()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if list[0].DefaultSortCol != "" {
		t.Fatalf("list default sort: %+v", list[0])
	}
}
