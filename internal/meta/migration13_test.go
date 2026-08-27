package meta

import (
	"fmt"
	"testing"
)

func TestMigration13StatCards(t *testing.T) {
	s := openTest(t)
	var v int
	var txt string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='schema_version'`).Scan(&txt); err != nil {
		t.Fatal(err)
	}
	fmt.Sscan(txt, &v)
	if v != len(migrations) || v != 13 {
		t.Fatalf("schema_version = %d (len(migrations)=%d), want 13", v, len(migrations))
	}
	var name string
	if err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='stat_cards'").Scan(&name); err != nil {
		t.Fatalf("stat_cards missing: %v", err)
	}
}

func TestStatCardsStore(t *testing.T) {
	s := openTest(t)
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "orders", Label: "Orders",
		KeyColumns: []string{"id"}, PageSize: 20}
	cols := []ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
	}
	if err := s.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	tdID := def.ID

	id1, err := s.CreateStatCard(tdID, "Revenue", "sum", "amount", "[]")
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := s.CreateStatCard(tdID, "Orders", "count", "", "[]")
	if id1 == id2 {
		t.Fatal("ids not distinct")
	}

	list, err := s.ListStatCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != id1 || list[1].ID != id2 {
		t.Fatalf("list order = %+v", list)
	}
	if list[0].Position >= list[1].Position {
		t.Fatalf("positions not increasing: %+v", list)
	}

	if err := s.MoveStatCard(id2, true); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListStatCards()
	if list[0].ID != id2 || list[1].ID != id1 {
		t.Fatalf("after move = %+v", list)
	}
	if err := s.MoveStatCard(id2, true); err == nil {
		t.Fatal("moving top card up should fail")
	}

	if err := s.UpdateStatCard(id1, "Revenue USD", "sum", "amount", `[]`); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetStatCard(id1)
	if err != nil || got.Label != "Revenue USD" {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := s.GetStatCard(999); err != ErrNotFound {
		t.Fatalf("missing = %v", err)
	}

	if err := s.DeleteStatCard(id1); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStatCard(id1); err != ErrNotFound {
		t.Fatal("double delete not ErrNotFound")
	}

	// cascade: deleting the def removes its cards
	s.CreateStatCard(tdID, "X", "count", "", "[]")
	if err := s.DeleteTableDef(tdID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListStatCards()
	if len(list) != 0 {
		t.Fatalf("cascade left cards: %+v", list)
	}
}
