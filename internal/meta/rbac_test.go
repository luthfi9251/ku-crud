package meta

import (
	"testing"
)

func seedDefs(t *testing.T, s *Store, n int) {
	t.Helper()
	if err := s.CreateDatasource(&Datasource{Name: "d", Host: "h", Port: 1, DBName: "db",
		Username: "u", Password: "p", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := s.SaveTableDef(&TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t",
			Label: "T", KeyColumns: []string{"id"}, PageSize: 20}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUserLifecycleWithRoles(t *testing.T) {
	s := openTest(t)
	seedDefs(t, s, 1)

	// first user -> admin + is_first
	if ok, _ := s.CreateFirstUser("first", "secret"); !ok {
		t.Fatal("first user not created")
	}
	// custom role
	r := &Role{Name: "Editor"}
	grants := []TableGrant{{TableDefID: 1, CanRead: true, CanCreate: true}}
	if err := s.CreateRole(r, grants); err != nil {
		t.Fatal(err)
	}
	if r.ID == 0 {
		t.Fatal("role id not set")
	}

	// create non-admin user with that role
	if err := s.CreateUserWithRole("bob", "pw123", r.ID); err != nil {
		t.Fatal(err)
	}

	u, ok, err := s.GetUserContext("bob")
	if err != nil || !ok {
		t.Fatalf("GetUserContext bob: %v %v", ok, err)
	}
	if u.IsAdmin || u.ManageDatasources || u.ManageTables || u.ViewAudit || u.ViewOutbox || u.RoleID != r.ID {
		t.Fatalf("bob context: %+v", u)
	}
	f, ok, _ := s.GetUserContext("first")
	if !ok || !f.IsAdmin || !f.IsFirst {
		t.Fatalf("first context: %+v %v", f, ok)
	}

	// disabled user: context lookup fails
	dis := true
	if err := s.UpdateUser(u.ID, nil, &dis, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetUserContext("bob"); ok {
		t.Fatal("disabled user should not resolve")
	}
	if ok, _ := s.VerifyUser("bob", "pw123"); ok {
		t.Fatal("disabled user should not verify")
	}

	// list users carries role names
	list, err := s.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("users=%d", len(list))
	}
	found := map[string]UserWithRole{}
	for _, u := range list {
		found[u.Username] = u
	}
	if found["bob"].RoleName != "Editor" || !found["bob"].Disabled {
		t.Fatalf("bob listing: %+v", found["bob"])
	}
	if found["first"].RoleName != "Admin" || !found["first"].IsFirst {
		t.Fatalf("first listing: %+v", found["first"])
	}

	// password change re-enables login after re-enable
	np := "newpw1"
	en := false
	if err := s.UpdateUser(u.ID, nil, &en, &np); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.VerifyUser("bob", "newpw1"); !ok {
		t.Fatal("password update failed")
	}

	// delete user
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("after delete users=%d", n)
	}
}

func TestRoleCRUDAndGuards(t *testing.T) {
	s := openTest(t)
	seedDefs(t, s, 8) // table defs 1..8 exist for grants
	r := &Role{Name: "Viewer", ManageTables: true}
	if err := s.CreateRole(r, []TableGrant{{TableDefID: 7, CanRead: true}}); err != nil {
		t.Fatal(err)
	}

	got, grants, err := s.GetRole(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Viewer" || !got.ManageTables || len(grants) != 1 || !grants[0].CanRead {
		t.Fatalf("role=%+v grants=%+v", got, grants)
	}

	// update replaces grants atomically
	r2 := &Role{ID: r.ID, Name: "Viewer2"}
	if err := s.UpdateRole(r2, []TableGrant{
		{TableDefID: 7, CanRead: true, CanUpdate: true},
		{TableDefID: 8, CanDelete: true},
	}); err != nil {
		t.Fatal(err)
	}
	_, grants, _ = s.GetRole(r.ID)
	if len(grants) != 2 {
		t.Fatalf("grants after update=%+v", grants)
	}

	// per-table grant lookup
	g, err := s.GrantsFor(r.ID, 7)
	if err != nil || !g.CanRead || !g.CanUpdate || g.CanDelete {
		t.Fatalf("GrantsFor=%+v err=%v", g, err)
	}
	if _, err := s.GrantsFor(r.ID, 999); err != ErrNotFound {
		t.Fatalf("GrantsFor missing: %v", err)
	}

	// builtin admin role is immutable
	admin := &Role{ID: 1, Name: "Hacked"}
	if err := s.UpdateRole(admin, nil); err != ErrImmutable {
		t.Fatalf("admin role update: %v", err)
	}
	if err := s.DeleteRole(1); err != ErrImmutable {
		t.Fatalf("admin role delete: %v", err)
	}

	// role in use cannot be deleted
	if err := s.CreateUserWithRole("u1", "pw12", r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(r.ID); err != ErrInUse {
		t.Fatalf("in-use delete: %v", err)
	}
	// after user deleted, role can go
	if err := s.DeleteUserByUsername("u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(r.ID); err != nil {
		t.Fatalf("delete after use: %v", err)
	}

	// listing includes user counts
	list, err := s.ListRoles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 { // only builtin Admin remains
		t.Fatalf("roles=%+v", list)
	}
}

func TestTableDefKeyColumns(t *testing.T) {
	s := openTest(t)
	seedDefs(t, s, 0)
	def := &TableDef{DatasourceID: 1, SchemaName: "public", TableName: "t", Label: "T",
		KeyColumns: []string{"a", "b"}, PageSize: 20}
	cols := []ColumnDef{
		{Name: "a", Label: "a", FieldType: "number", Editable: true, Visible: true, Position: 0},
		{Name: "b", Label: "b", FieldType: "text", Editable: true, Visible: true, Position: 1},
	}
	if err := s.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	got, gcols, err := s.GetTableDef(def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.KeyColumns) != 2 || got.KeyColumns[0] != "a" || got.KeyColumns[1] != "b" {
		t.Fatalf("keyColumns=%v", got.KeyColumns)
	}
	if len(gcols) != 2 {
		t.Fatalf("cols=%d", len(gcols))
	}
	list, _ := s.ListTableDefs()
	if len(list) != 1 || len(list[0].KeyColumns) != 2 {
		t.Fatalf("list=%+v", list)
	}
	// update with different keys
	def.KeyColumns = []string{"b"}
	if err := s.UpdateTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetTableDef(def.ID)
	if len(got.KeyColumns) != 1 || got.KeyColumns[0] != "b" {
		t.Fatalf("after update=%v", got.KeyColumns)
	}
}
