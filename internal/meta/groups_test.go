package meta

import "testing"

func TestGroupCRUDAndOrdering(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateGroup("Sales")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateGroup("Ops")
	if err != nil {
		t.Fatal(err)
	}
	gs, err := s.ListGroups()
	if err != nil || len(gs) != 2 || gs[0].Name != "Sales" || gs[1].Name != "Ops" {
		t.Fatalf("list = %+v err=%v", gs, err)
	}
	if _, err := s.CreateGroup("Sales"); err != ErrGroupTaken {
		t.Fatalf("duplicate name err=%v", err)
	}
	if err := s.RenameGroup(b, "HR"); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveGroup(b, -1); err != nil {
		t.Fatal(err)
	}
	gs, _ = s.ListGroups()
	if gs[0].Name != "HR" || gs[1].Name != "Sales" {
		t.Fatalf("move failed: %+v", gs)
	}
	// assign a table, delete group -> table survives ungrouped
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t", Label: "T",
		KeyColumns: []string{"id"}, PageSize: 20, GroupID: a}
	if err := s.SaveTableDef(def, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(a); err != nil {
		t.Fatal(err)
	}
	d, _, _ := s.GetTableDef(def.ID)
	if d == nil || d.GroupID != 0 {
		t.Fatalf("group_id not nulled after group delete: %+v", d)
	}
}

func TestSetTableGroup(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	g, _ := s.CreateGroup("G")
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t", Label: "T",
		KeyColumns: []string{"id"}, PageSize: 20}
	s.SaveTableDef(def, nil)
	if err := s.SetTableGroup(def.ID, g); err != nil {
		t.Fatal(err)
	}
	d, _, _ := s.GetTableDef(def.ID)
	if d.GroupID != g {
		t.Fatal("group not set")
	}
	s.SetTableGroup(def.ID, 0)
	d, _, _ = s.GetTableDef(def.ID)
	if d.GroupID != 0 {
		t.Fatal("group not cleared")
	}
}
