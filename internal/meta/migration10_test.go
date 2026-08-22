package meta

import "testing"

func TestMigration10Schema(t *testing.T) {
	s := openTest(t)
	// four grant columns exist, platform_manage is gone
	for _, col := range []string{"manage_datasources", "manage_tables", "view_audit", "view_outbox"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('roles') WHERE name=?`, col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("roles.%s missing (n=%d err=%v)", col, n, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('roles') WHERE name='platform_manage'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("roles.platform_manage must be dropped (n=%d err=%v)", n, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('table_defs') WHERE name='description'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("table_defs.description missing (n=%d err=%v)", n, err)
	}
	// builtin Admin got all four grants from the backfill
	var ds, tb, au, ob int
	if err := s.db.QueryRow(`SELECT manage_datasources,manage_tables,view_audit,view_outbox FROM roles WHERE is_admin=1`).
		Scan(&ds, &tb, &au, &ob); err != nil {
		t.Fatal(err)
	}
	if ds != 1 || tb != 1 || au != 1 || ob != 1 {
		t.Fatalf("admin backfill = %d %d %d %d", ds, tb, au, ob)
	}
}

func TestRoleGrantRoundtrip(t *testing.T) {
	s := openTest(t)
	r := &Role{Name: "Ops", ManageDatasources: true, ViewAudit: true}
	if err := s.CreateRole(r, nil); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.GetRole(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ManageDatasources || got.ManageTables || !got.ViewAudit || got.ViewOutbox {
		t.Fatalf("roundtrip: %+v", got)
	}
	if err := s.UpdateRole(&Role{ID: r.ID, Name: "Ops", ViewOutbox: true}, nil); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetRole(r.ID)
	if got.ManageDatasources || got.ManageTables || got.ViewAudit || !got.ViewOutbox {
		t.Fatalf("update: %+v", got)
	}
	list, err := s.ListRoles()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, x := range list {
		if x.ID == r.ID && x.ViewOutbox {
			found = true
		}
	}
	if !found {
		t.Fatal("ListRoles did not return the updated flags")
	}
}

func TestUserCtxCarriesGrants(t *testing.T) {
	s := openTest(t)
	r := &Role{Name: "Auditor", ViewAudit: true}
	if err := s.CreateRole(r, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUserWithRole("aud", "pw123", r.ID); err != nil {
		t.Fatal(err)
	}
	u, ok, err := s.GetUserContext("aud")
	if err != nil || !ok {
		t.Fatalf("context: %v %v", ok, err)
	}
	if u.IsAdmin || u.ManageDatasources || u.ManageTables || u.ViewOutbox || !u.ViewAudit {
		t.Fatalf("aud ctx: %+v", u)
	}
}
