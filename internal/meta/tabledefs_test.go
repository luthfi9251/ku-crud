package meta

import "testing"

func TestTableDefCRUD(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", PKColumn: "id", PageSize: 20}
	cols := []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "status", Label: "Status", FieldType: "enum", EnumOptions: []string{"sunny", "rainy"},
			Editable: true, Visible: true, Position: 1},
	}
	if err := s.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	if def.ID == 0 {
		t.Fatal("def id not set")
	}
	gotDef, gotCols, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDef.Label != "Customers" || len(gotCols) != 2 || gotCols[1].EnumOptions[1] != "rainy" {
		t.Fatalf("got %+v %+v", gotDef, gotCols)
	}

	gotDef.Label = "Customers v2"
	gotCols[0].Label = "ID#"
	if err := s.UpdateTableDef(gotDef, gotCols); err != nil {
		t.Fatal(err)
	}
	_, gotCols2, _ := s.GetTableDef(def.ID)
	if gotCols2[0].Label != "ID#" || len(gotCols2) != 2 {
		t.Fatalf("update lost: %+v", gotCols2)
	}
	if list, _ := s.ListTableDefs(); len(list) != 1 {
		t.Fatalf("list=%d", len(list))
	}

	// ReplaceColumns (resync path): drop status, keep id settings
	if err := s.ReplaceColumns(def.ID, gotCols2[:1]); err != nil {
		t.Fatal(err)
	}
	_, gotCols3, _ := s.GetTableDef(def.ID)
	if len(gotCols3) != 1 || gotCols3[0].Name != "id" {
		t.Fatalf("replace: %+v", gotCols3)
	}

	if err := s.DeleteTableDef(def.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetTableDef(def.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
