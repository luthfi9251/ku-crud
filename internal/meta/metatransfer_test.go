package meta

import "testing"

func plannedDef(ds, schema, table string, cols []PlannedColumn) PlannedDef {
	return PlannedDef{Def: TableDef{SchemaName: schema, TableName: table, Label: table,
		KeyColumns: []string{"id"}, PageSize: 50, DefaultSortDir: "ASC"}, DsName: ds, Columns: cols}
}

func col(name, ft string) ColumnDef {
	return ColumnDef{Name: name, Label: name, FieldType: ft, Editable: true, Visible: true, Position: 1}
}

func TestApplyImportFKRemapAndGroups(t *testing.T) {
	s := openTest(t)
	// local datasource + local "customers" def as fk target
	s.CreateDatasource(&Datasource{Name: "pg1", Host: "h", Port: 1, DBName: "d", Username: "u", Password: "x"})
	var dsID int64 = 1
	cust := &TableDef{DatasourceID: dsID, SchemaName: "public", TableName: "customers", Label: "Customers",
		KeyColumns: []string{"id"}, PageSize: 50}
	s.SaveTableDef(cust, []ColumnDef{col("id", "number")})

	// plan: new group, new ds, new def "orders" with fk -> local customers
	ordersDef := plannedDef("pg1", "public", "orders", []PlannedColumn{
		{Col: col("id", "number")},
		{Col: ColumnDef{Name: "customer_id", Label: "C", FieldType: "fk", BaseType: "number",
			Position: 2, FKRefColumn: "id", FKDisplayColumns: []string{"id"}},
			FKRef: DefRef{DsName: "pg1", Schema: "public", Table: "customers"}},
	})
	ordersDef.GroupName = "Sales"
	plan := ImportPlan{
		Groups:      []string{"Sales"},
		Datasources: []PlannedDatasource{{DS: Datasource{Name: "pg1", Host: "h2", Port: 2, DBName: "d2", Username: "u2"}, Password: "newpass"}},
		Defs:        []PlannedDef{ordersDef},
	}
	created, updated, err := s.ApplyImport(plan)
	if err != nil || created != 1 || updated != 0 {
		t.Fatalf("apply: created=%d updated=%d err=%v", created, updated, err)
	}
	defs, _ := s.ListTableDefs()
	var orders *TableDef
	for i := range defs {
		if defs[i].TableName == "orders" {
			orders = &defs[i]
		}
	}
	if orders == nil {
		t.Fatal("orders def not created")
	}
	if orders.GroupID == 0 {
		t.Fatal("group not assigned")
	}
	_, ocols, _ := s.GetTableDef(orders.ID)
	if ocols[1].FKTableDefID != cust.ID {
		t.Fatalf("fk not remapped to local def: %d want %d", ocols[1].FKTableDefID, cust.ID)
	}
	// ds password updated on overwrite-mode
	got, _ := s.GetDatasource(dsID)
	if got.Password != "newpass" {
		t.Fatalf("password not applied: %q", got.Password)
	}
}

func TestApplyImportSelfRefAndBundleTarget(t *testing.T) {
	s := openTest(t)
	s.CreateDatasource(&Datasource{Name: "pg1", Host: "h", Port: 1, DBName: "d", Username: "u", Password: "x"})
	// two new defs referencing each other + one self-referencing column
	plan := ImportPlan{
		Datasources: []PlannedDatasource{{LocalID: 1, DS: Datasource{Name: "pg1", Host: "h", Port: 1, DBName: "d", Username: "u"}}},
		Defs: []PlannedDef{
			plannedDef("pg1", "public", "a", []PlannedColumn{
				{Col: col("id", "number")},
				{Col: ColumnDef{Name: "b_id", Label: "B", FieldType: "fk", BaseType: "number", Position: 2,
					FKRefColumn: "id", FKDisplayColumns: []string{"id"}},
					FKRef: DefRef{DsName: "pg1", Schema: "public", Table: "b"}},
				{Col: ColumnDef{Name: "parent_id", Label: "P", FieldType: "fk", BaseType: "number", Position: 3,
					FKRefColumn: "id", FKDisplayColumns: []string{"id"}},
					FKRef: DefRef{DsName: "pg1", Schema: "public", Table: "a"}}, // self
			}),
			plannedDef("pg1", "public", "b", []PlannedColumn{{Col: col("id", "number")}}),
		},
	}
	if _, _, err := s.ApplyImport(plan); err != nil {
		t.Fatal(err)
	}
	defs, _ := s.ListTableDefs()
	byName := map[string]int64{}
	for _, d := range defs {
		byName[d.TableName] = d.ID
	}
	_, acols, _ := s.GetTableDef(byName["a"])
	if acols[1].FKTableDefID != byName["b"] {
		t.Fatalf("bundle fk remap: %d want %d", acols[1].FKTableDefID, byName["b"])
	}
	if acols[2].FKTableDefID != byName["a"] {
		t.Fatalf("self fk remap: %d want %d", acols[2].FKTableDefID, byName["a"])
	}
}

func TestApplyImportRollbackOnBadRef(t *testing.T) {
	s := openTest(t)
	s.CreateDatasource(&Datasource{Name: "pg1", Host: "h", Port: 1, DBName: "d", Username: "u", Password: "x"})
	plan := ImportPlan{
		Defs: []PlannedDef{plannedDef("pg1", "public", "x", []PlannedColumn{
			{Col: ColumnDef{Name: "y", Label: "Y", FieldType: "fk", BaseType: "number", Position: 1,
				FKRefColumn: "id", FKDisplayColumns: []string{"id"}},
				FKRef: DefRef{DsName: "pg1", Schema: "public", Table: "ghost"}},
		})},
	}
	if _, _, err := s.ApplyImport(plan); err == nil {
		t.Fatal("unresolvable ref must fail")
	}
	defs, _ := s.ListTableDefs()
	if len(defs) != 0 {
		t.Fatalf("rollback failed, defs left: %d", len(defs))
	}
}
