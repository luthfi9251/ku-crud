package meta

import (
	"testing"
	"time"
)

func mustDef(t *testing.T, s *Store) int64 {
	t.Helper()
	d := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
		Label: "T", KeyColumns: []string{"id"}, PageSize: 20, Hooks: `{"beforeCreate":[{"hook":"X","order":1}]}`}
	if err := s.SaveTableDef(d, nil); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func TestTableDefHooksRoundtrip(t *testing.T) {
	s := openTest(t)
	s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db", Username: "u", Password: "p"})
	id := mustDef(t, s)
	d, _, err := s.GetTableDef(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Hooks != `{"beforeCreate":[{"hook":"X","order":1}]}` {
		t.Fatalf("hooks roundtrip = %q", d.Hooks)
	}
	d.Hooks = `{"afterDelete":[{"hook":"Y","order":1}]}`
	if err := s.UpdateTableDef(d, nil); err != nil {
		t.Fatal(err)
	}
	d, _, _ = s.GetTableDef(id)
	if d.Hooks != `{"afterDelete":[{"hook":"Y","order":1}]}` {
		t.Fatalf("hooks update = %q", d.Hooks)
	}
	list, _ := s.ListTableDefs()
	if list[0].Hooks == "" {
		t.Fatal("ListTableDefs must select hooks")
	}
}

func TestOutboxLifecycle(t *testing.T) {
	s := openTest(t)
	s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db", Username: "u", Password: "p"})
	id := mustDef(t, s)

	e := &OutboxEntry{TableDefID: id, Event: "afterCreate", HookName: "H",
		OldValues: `{"a":1}`, NewValues: `{"b":2}`, Config: `{"k":1}`,
		UserID: 7, Username: "admin"}
	if err := s.EnqueueOutbox(e); err != nil {
		t.Fatal(err)
	}
	if e.ID == 0 {
		t.Fatal("id not set")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	due, err := s.DueOutbox(now, 10)
	if err != nil || len(due) != 1 || due[0].HookName != "H" || due[0].Username != "admin" {
		t.Fatalf("due = %v %v", due, err)
	}
	if err := s.MarkOutboxDone(e.ID); err != nil {
		t.Fatal(err)
	}
	// done entries never come back
	if due, _ = s.DueOutbox(now, 10); len(due) != 0 {
		t.Fatal("done entry still due")
	}
	got, _ := s.GetOutbox(e.ID)
	if got.Status != "done" {
		t.Fatalf("status = %q", got.Status)
	}
	// done entries cannot be manually retried
	if err := s.RetryOutbox(e.ID); err != ErrNotFound {
		t.Fatalf("retry done = %v, want ErrNotFound", err)
	}
	if got, _ = s.GetOutbox(e.ID); got.Status != "done" {
		t.Fatalf("done entry was reset by retry: %+v", got)
	}

	// failed → scheduled retry → due at that time, dead after 6
	e2 := &OutboxEntry{TableDefID: id, Event: "afterCreate", HookName: "H"}
	s.EnqueueOutbox(e2)
	s.MarkOutboxFailed(e2.ID, 1, now, "boom") // attempts=1 → retry scheduled
	due, _ = s.DueOutbox(now, 10)
	if len(due) != 1 || due[0].Attempts != 1 || due[0].LastError != "boom" {
		t.Fatalf("retry state = %+v", due)
	}
	s.MarkOutboxFailed(e2.ID, 6, "", "dead") // empty next → dead
	got, _ = s.GetOutbox(e2.ID)
	if got.Status != "dead" || got.NextRetryAt != "" {
		t.Fatalf("dead = %+v", got)
	}
	if due, _ = s.DueOutbox(now, 10); len(due) != 0 {
		t.Fatal("dead entry must not be due")
	}
	s.RetryOutbox(e2.ID)
	got, _ = s.GetOutbox(e2.ID)
	if got.Status != "pending" || got.Attempts != 0 {
		t.Fatalf("after retry = %+v", got)
	}

	// list + filter + total
	list, total, err := s.ListOutbox("", 0, 50, 0)
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("list = %d %v %v", total, list, err)
	}
	list, total, _ = s.ListOutbox("done", 0, 50, 0)
	if total != 1 || list[0].Status != "done" {
		t.Fatalf("status filter = %d %+v", total, list)
	}
}
