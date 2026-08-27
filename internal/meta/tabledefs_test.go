package meta

import (
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
)

func TestTableDefCRUD(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20}
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

func TestColumnDefFKRoundTrip(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	parent := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(parent, []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}

	// orders.customer_id is an FK to customers.id (plain numeric target id)
	orders := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 20}
	cols := []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Position: 0},
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			FKTableDefID: parent.ID, FKRefColumn: "id", FKDisplayColumns: []string{"name"},
			Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 1},
	}
	if err := s.SaveTableDef(orders, cols); err != nil {
		t.Fatal(err)
	}
	_, got, err := s.GetTableDef(orders.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("get: %v %+v", err, got)
	}
	fk := got[1]
	if fk.FieldType != "fk" || fk.BaseType != "number" || fk.FKTableDefID != parent.ID ||
		fk.FKRefColumn != "id" || len(fk.FKDisplayColumns) != 1 || fk.FKDisplayColumns[0] != "name" {
		t.Fatalf("fk round-trip lost: %+v", fk)
	}

	// self-reference: categories.parent_id → same def, sent as SelfRef
	cats := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "categories",
		Label: "Categories", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(cats, []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Position: 0},
		{Name: "parent_id", Label: "Parent", FieldType: "fk", BaseType: "number",
			FKTableDefID: SelfRef, FKRefColumn: "id", FKDisplayColumns: []string{"id"},
			Editable: true, Visible: true, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	_, got2, _ := s.GetTableDef(cats.ID)
	if got2[1].FKTableDefID != cats.ID {
		t.Fatalf("SelfRef not resolved: %+v", got2[1])
	}
}

func TestFKRefSources(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	parent := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(parent, []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Position: 0}}); err != nil {
		t.Fatal(err)
	}
	child := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(child, []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Position: 0},
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			FKTableDefID: parent.ID, FKRefColumn: "id", FKDisplayColumns: []string{"id"},
			Editable: true, Visible: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}
	// a table that references the child (no reference to parent)
	other := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "notes",
		Label: "Notes", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(other, []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Position: 0},
		{Name: "order_id", Label: "Order", FieldType: "fk", BaseType: "number",
			FKTableDefID: child.ID, FKRefColumn: "id", FKDisplayColumns: []string{"id"},
			Editable: true, Visible: true, Position: 1}}); err != nil {
		t.Fatal(err)
	}

	srcs, err := s.FKRefSources(parent.ID)
	if err != nil || len(srcs) != 1 {
		t.Fatalf("parent sources: %v %+v", err, srcs)
	}
	if srcs[0].DefID != child.ID || srcs[0].DefLabel != "Orders" ||
		srcs[0].Column != "customer_id" || srcs[0].RefColumn != "id" {
		t.Fatalf("source: %+v", srcs[0])
	}
	if srcs, _ := s.FKRefSources(other.ID); len(srcs) != 0 {
		t.Fatalf("leaf table should have no sources: %+v", srcs)
	}
}

func TestTableDefDescription(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
		Label: "T", KeyColumns: []string{"id"}, PageSize: 20, Description: "Daily orders"}
	if err := s.SaveTableDef(def, nil); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Daily orders" {
		t.Fatalf("description after save: %q", got.Description)
	}
	list, err := s.ListTableDefs()
	if err != nil || len(list) != 1 || list[0].Description != "Daily orders" {
		t.Fatalf("list: %v %+v", err, list)
	}
	def.Description = "All orders"
	if err := s.UpdateTableDef(def, nil); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetTableDef(def.ID)
	if got.Description != "All orders" {
		t.Fatalf("description after update: %q", got.Description)
	}
}

func TestToCoreDefDanglingRefs(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	target := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "tags",
		Label: "Tags", KeyColumns: []string{"id"}, PageSize: 20}
	junction := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customer_tags",
		Label: "CT", KeyColumns: []string{"customer_id", "tag_id"}, PageSize: 20}
	src := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.SaveTableDef(target, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTableDef(junction, []ColumnDef{
		{Name: "customer_id", FieldType: "fk", FKTableDefID: 3, FKRefColumn: "id"},
		{Name: "tag_id", FieldType: "fk", FKTableDefID: 1, FKRefColumn: "id"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTableDef(src, []ColumnDef{
		{Name: "self_ref", FieldType: "fk", FKTableDefID: 3, FKRefColumn: "id"},
		{Name: "fk_tag", FieldType: "fk", FKTableDefID: 1, FKRefColumn: "id"},
		{Name: "m2m_tags", FieldType: "m2m", M2MJunctionDefID: 2,
			M2MJunctionSrcCol: "customer_id", M2MJunctionTgtCol: "tag_id"},
	}); err != nil {
		t.Fatal(err)
	}
	buildIDToName := func() map[int64]string {
		list, err := s.ListTableDefs()
		if err != nil {
			t.Fatal(err)
		}
		m := map[int64]string{}
		for _, d := range list {
			m[d.ID] = d.TableName
		}
		return m
	}

	// delete the target def: its id dangles in fk_tag and the junction's
	// tag_id, while self_ref and the junction id stay intact
	if err := s.DeleteTableDef(1); err != nil {
		t.Fatal(err)
	}
	def, cols, err := s.GetTableDef(3)
	if err != nil {
		t.Fatal(err)
	}
	ct := ToCoreDef(*def, cols, buildIDToName())
	var fkSelf, fkTag *defs.FK
	var m2m *defs.M2M
	for i := range ct.Columns {
		switch ct.Columns[i].Name {
		case "self_ref":
			fkSelf = ct.Columns[i].FK
		case "fk_tag":
			fkTag = ct.Columns[i].FK
		case "m2m_tags":
			m2m = ct.Columns[i].M2M
		}
	}
	if fkSelf == nil || fkSelf.Table != "" {
		t.Fatalf("self fk table = %+v (must stay the \"\" self convention)", fkSelf)
	}
	if fkTag == nil || fkTag.Table != defs.MissingTable {
		t.Fatalf("dangling fk table = %+v (must be defs.MissingTable)", fkTag)
	}
	if m2m == nil || m2m.JunctionTable != "customer_tags" {
		t.Fatalf("junction mapping changed: %+v", m2m)
	}

	// delete the junction too: the m2m ref dangles as well
	if err := s.DeleteTableDef(2); err != nil {
		t.Fatal(err)
	}
	def, cols, err = s.GetTableDef(3)
	if err != nil {
		t.Fatal(err)
	}
	ct = ToCoreDef(*def, cols, buildIDToName())
	for i := range ct.Columns {
		if ct.Columns[i].Name == "m2m_tags" {
			m2m = ct.Columns[i].M2M
		}
	}
	if m2m == nil || m2m.JunctionTable != defs.MissingTable {
		t.Fatalf("dangling junction = %+v (must be defs.MissingTable)", m2m)
	}
}
