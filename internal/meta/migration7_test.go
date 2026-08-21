package meta

import "testing"

func TestMigration7Schema(t *testing.T) {
	s := openTest(t)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='table_groups'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("table_groups missing (n=%d err=%v)", n, err)
	}
	for _, tc := range []struct{ tbl, col string }{{"table_defs", "group_id"}, {"columns", "validations"}} {
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, tc.tbl, tc.col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s.%s missing (n=%d err=%v)", tc.tbl, tc.col, n, err)
		}
	}
}

func TestValidationsRoundtrip(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db", Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	cols := []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Visible: true, Position: 1,
			Validations: []ValidationRule{{Type: "number"}}},
		{Name: "email", Label: "Email", FieldType: "text", Editable: true, Visible: true, Position: 2,
			Validations: []ValidationRule{{Type: "email"}, {Type: "max_len", Param: 100}}},
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t", Label: "T", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	_, got, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[1].Validations) != 2 || got[1].Validations[1].Param != 100 {
		t.Fatalf("validations roundtrip mismatch: %+v", got)
	}
}
