package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestMetaExport(t *testing.T) {
	s := newTestServer(t)
	c := login(s)

	// seed: 1 datasource, 2 defs (customers, orders with fk -> customers + email validation)
	ds := &meta.Datasource{
		Name: "pg1", Host: "h", Port: 5432, DBName: "db", Username: "u", Password: "SECRET", Driver: "postgres",
	}
	if err := s.store.CreateDatasource(ds); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.store.CreateGroup("Sales")
	cust := &meta.TableDef{DatasourceID: ds.ID, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 50, DefaultSortCol: "id", GroupID: gid}
	if err := s.store.SaveTableDef(cust, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "email", Label: "Email", FieldType: "text", Editable: true, Visible: true, Position: 2,
			Validations: []meta.ValidationRule{{Type: "email"}}},
	}); err != nil {
		t.Fatal(err)
	}
	orders := &meta.TableDef{DatasourceID: ds.ID, SchemaName: "public", TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 50}
	if err := s.store.SaveTableDef(orders, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Visible: true, Position: 1},
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			Editable: true, Visible: true, Position: 2,
			FKTableDefID: cust.ID, FKRefColumn: "id", FKDisplayColumns: []string{"email"}},
	}); err != nil {
		t.Fatal(err)
	}

	w := do(s, "GET", "/api/meta/export", "", c)
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if strings.Contains(body, "SECRET") || strings.Contains(strings.ToLower(body), "password") {
		t.Fatalf("password leaked in export: %s", body)
	}
	var f metaFile
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("not valid meta file: %v\n%s", err, body)
	}
	if f.Format != "ku-crud-meta" || f.Version != 1 || len(f.Datasources) != 1 || len(f.Tables) != 2 || len(f.Groups) != 1 {
		t.Fatalf("header wrong: %+v", f)
	}
	var ord *metaFileTable
	for i := range f.Tables {
		if f.Tables[i].Table == "orders" {
			ord = &f.Tables[i]
		}
	}
	if ord == nil || ord.Columns[1].FKTableRef == nil ||
		ord.Columns[1].FKTableRef.DatasourceRef != "pg1" ||
		ord.Columns[1].FKTableRef.Table != "customers" {
		t.Fatalf("fkTableRef not natural-keyed: %+v", ord)
	}
	if f.Tables[0].GroupRef != "Sales" {
		t.Fatalf("groupRef missing: %+v", f.Tables[0])
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "ku-crud-meta-") {
		t.Fatalf("attachment header: %q", cd)
	}
}
