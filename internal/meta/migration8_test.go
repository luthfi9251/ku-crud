package meta

import "testing"

func TestMigration8Schema(t *testing.T) {
	s := openTest(t)
	// seed parent rows so the saved_filters FK inserts below are valid
	if _, err := s.db.Exec(`INSERT INTO users(username,password_hash) VALUES('u','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode) VALUES('d','h',1,'db','u','p','disable')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,key_columns,page_size) VALUES(1,'public','t','T','[]',20)`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tbl, col string }{
		{"columns", "formatting"}, {"columns", "is_computed"}, {"columns", "computed_formula"},
		{"table_defs", "default_view"}, {"table_defs", "view_config"},
		{"users", "language"},
	} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, tc.tbl, tc.col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s.%s missing (n=%d err=%v)", tc.tbl, tc.col, n, err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO saved_filters(user_id,table_def_id,name,filters) VALUES(1,1,'x','[]')`); err != nil {
		t.Fatalf("saved_filters not usable: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO saved_filters(user_id,table_def_id,name,filters) VALUES(1,1,'x','[]')`); err == nil {
		t.Fatal("duplicate (user,table,name) must be rejected by the unique index")
	}
}
