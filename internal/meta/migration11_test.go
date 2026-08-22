package meta

import "testing"

func TestMigration11QueryDefs(t *testing.T) {
	st, err := Open(t.TempDir() + "/m11.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1,
		DBName: "b", Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SourceType: "query",
		QuerySQL: "SELECT name AS n, balance FROM customers",
		Label: "Q", KeyColumns: []string{}, PageSize: 20}
	cols := []ColumnDef{{Name: "n", Label: "N", FieldType: "text",
		Visible: true, Searchable: true, Sortable: true, Position: 0}}
	if err := st.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	got, gcols, err := st.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceType != "query" || got.QuerySQL != def.QuerySQL || len(gcols) != 1 {
		t.Fatalf("round-trip = %+v cols=%d", got, len(gcols))
	}
	list, err := st.ListTableDefs()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v %v", list, err)
	}
	if list[0].SourceType != "query" {
		t.Fatalf("list sourceType = %q", list[0].SourceType)
	}
	// legacy rows read as "table"
	if err := st.UpdateTableDef(&TableDef{ID: def.ID, DatasourceID: 1,
		SchemaName: "s", TableName: "t", Label: "L", KeyColumns: []string{"n"},
		PageSize: 20, SourceType: "table", QuerySQL: ""}, cols); err != nil {
		t.Fatal(err)
	}
	got, _, _ = st.GetTableDef(def.ID)
	if got.SourceType != "table" {
		t.Fatalf("table def sourceType = %q", got.SourceType)
	}
}
