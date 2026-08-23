package meta

import "testing"

const testActions = `{"hidden":["copy","refresh"],"custom":[{"id":"ship","label":"Ship it","confirm":"Ship now?","grant":"update","hook":"ShipOrder","config":{"courier":"dpd"},"order":1,"style":"primary"}]}`

func TestTableDefActionsRoundtrip(t *testing.T) {
	s := openTest(t)
	s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db", Username: "u", Password: "p"})
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 20, Actions: testActions}
	if err := s.SaveTableDef(def, nil); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Actions != testActions {
		t.Fatalf("actions roundtrip = %q", got.Actions)
	}
	got.Actions = `{"hidden":["export"]}`
	if err := s.UpdateTableDef(got, nil); err != nil {
		t.Fatal(err)
	}
	got2, _, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Actions != `{"hidden":["export"]}` {
		t.Fatalf("actions update = %q", got2.Actions)
	}
	list, err := s.ListTableDefs()
	if err != nil || list[0].Actions != `{"hidden":["export"]}` {
		t.Fatalf("list actions = %q %v", list[0].Actions, err)
	}
}
