package meta_test

import (
	"reflect"
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
	"ku-crud/internal/meta"
)

func TestTableDefToCoreRoundTrip(t *testing.T) {
	md := meta.TableDef{ID: 7, DatasourceID: 3, SchemaName: "public",
		TableName: "products", Label: "Products", KeyColumns: []string{"id"},
		PageSize: 25, DefaultSortCol: "created_at", DefaultSortDir: "desc"}
	mcols := []meta.ColumnDef{
		{Name: "category_id", FieldType: "fk", FKTableDefID: 9, FKRefColumn: "id",
			FKDisplayColumns: []string{"name"}, Editable: true},
		{Name: "parent_id", FieldType: "fk", FKTableDefID: 7, FKRefColumn: "id"},
		{Name: "tags", FieldType: "m2m", M2MJunctionDefID: 12, M2MJunctionSrcCol: "product_id",
			M2MJunctionTgtCol: "tag_id", M2MDisplayColumns: []string{"name"}},
	}
	ids := map[int64]string{7: "products", 9: "categories", 12: "product_tags"}

	got := meta.ToCoreDef(md, mcols, ids)

	if got.Name != "products" || len(got.Columns) != 3 {
		t.Fatalf("bad table: %+v", got)
	}
	fk := got.Columns[0].FK
	if fk == nil || fk.Table != "categories" || fk.RefColumn != "id" {
		t.Fatalf("fk not name-based: %+v", fk)
	}
	selfFK := got.Columns[1].FK
	if selfFK == nil || selfFK.Table != "" { // "" = self, per the defs.FK contract
		t.Fatalf("self fk: %+v", selfFK)
	}
	m2m := got.Columns[2].M2M
	if m2m == nil || m2m.JunctionTable != "product_tags" ||
		m2m.SrcCol != "product_id" || m2m.TgtCol != "tag_id" ||
		!reflect.DeepEqual(m2m.DisplayColumns, []string{"name"}) {
		t.Fatalf("m2m not name-based: %+v", m2m)
	}
}

func TestToCoreDefCoversEngineReadFields(t *testing.T) {
	md := meta.TableDef{ID: 3, DatasourceID: 1, SchemaName: "sales", TableName: "orders",
		Label: "Sales Orders", Description: "customer orders",
		KeyColumns: []string{"org_id", "id"}, PageSize: 50,
		DefaultSortCol: "id", DefaultSortDir: "DESC",
		DefaultView: "kanban", ViewConfig: `{"group_by":"status"}`,
		SourceType: "query", QuerySQL: "SELECT * FROM v_orders",
		Hooks: `{"after_insert":"h1"}`, Actions: `[{"id":"a1"}]`}
	mcols := []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Required: true, Visible: true,
			Sortable: true, Position: 1, BaseType: "int"},
		{Name: "status", Label: "Status", FieldType: "enum", EnumOptions: []string{"open", "closed"},
			Editable: true, Searchable: true, Position: 2,
			Validations: []meta.ValidationRule{{Type: "min_len", Param: 2}}},
		{Name: "note", Label: "Note", FieldType: "text", Position: 3, Formatting: `{"mask":"upper"}`},
		{Name: "total", Label: "Total", FieldType: "text", Position: 4, IsComputed: true,
			ComputedFormula: "qty * price"},
		{Name: "customer_id", FieldType: "fk", FKTableDefID: 3, FKRefColumn: "id",
			FKDisplayColumns: []string{"name"}},
		{Name: "items", FieldType: "m2m", M2MJunctionDefID: 8, M2MJunctionSrcCol: "order_id",
			M2MJunctionTgtCol: "item_id", M2MDisplayColumns: []string{"sku"}},
	}
	ids := map[int64]string{3: "orders", 8: "order_items"}

	got := meta.ToCoreDef(md, mcols, ids)

	if got.Name != "orders" || got.PhysTab != "orders" || got.Schema != "sales" ||
		got.Label != "Sales Orders" || got.Description != "customer orders" {
		t.Fatalf("table identity fields: %+v", got)
	}
	if !reflect.DeepEqual(got.Keys, []string{"org_id", "id"}) || got.PageSize != 50 ||
		got.DefaultSortCol != "id" || got.DefaultSortDir != "DESC" {
		t.Fatalf("list defaults: %+v", got)
	}
	if got.DefaultView != "kanban" || got.ViewConfig != `{"group_by":"status"}` ||
		got.SourceType != "query" || got.QuerySQL != "SELECT * FROM v_orders" ||
		got.Hooks != `{"after_insert":"h1"}` || got.Actions != `[{"id":"a1"}]` {
		t.Fatalf("view/source/hook fields: %+v", got)
	}

	num := got.Columns[0]
	if num.Label != "ID" || num.FieldType != "number" || num.Editable || !num.Required ||
		!num.Visible || num.Searchable || !num.Sortable || num.Position != 1 || num.BaseType != "int" ||
		num.FK != nil || num.M2M != nil {
		t.Fatalf("number column: %+v", num)
	}
	enm := got.Columns[1]
	if !reflect.DeepEqual(enm.EnumOptions, []string{"open", "closed"}) || !enm.Editable ||
		!enm.Searchable || len(enm.Validations) != 1 ||
		(enm.Validations[0] != defs.ValidationRule{Type: "min_len", Param: 2}) {
		t.Fatalf("enum column: %+v", enm)
	}
	if got.Columns[2].Formatting != `{"mask":"upper"}` {
		t.Fatalf("formatting: %+v", got.Columns[2])
	}
	cmp := got.Columns[3]
	if !cmp.IsComputed || cmp.ComputedFormula != "qty * price" {
		t.Fatalf("computed column: %+v", cmp)
	}
	if got.Columns[4].FK == nil || got.Columns[4].FK.Table != "" ||
		got.Columns[4].FK.RefColumn != "id" ||
		!reflect.DeepEqual(got.Columns[4].FK.DisplayColumns, []string{"name"}) {
		t.Fatalf("self fk display fields: %+v", got.Columns[4].FK)
	}
	if got.Columns[5].M2M == nil || got.Columns[5].M2M.JunctionTable != "order_items" {
		t.Fatalf("m2m junction name: %+v", got.Columns[5].M2M)
	}
}
