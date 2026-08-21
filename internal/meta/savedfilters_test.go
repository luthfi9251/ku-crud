package meta

import "testing"

func TestSavedFiltersCRUD(t *testing.T) {
	s := openTest(t)
	// seed parent rows so the saved_filters FK inserts below are valid
	for _, u := range []string{"u1", "u2"} {
		if _, err := s.db.Exec(`INSERT INTO users(username,password_hash) VALUES(?,'x')`, u); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode) VALUES('d','h',1,'db','u','p','disable')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,key_columns,page_size) VALUES(1,'public','t','T','[]',20)`); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateSavedFilter(1, 1, "Recent", `[{"column":"status","op":"eq","values":["open"]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSavedFilter(1, 1, "Recent", `[]`); err != ErrFilterTaken {
		t.Fatalf("duplicate name err=%v", err)
	}
	if _, err := s.CreateSavedFilter(2, 1, "Recent", `[]`); err != nil {
		t.Fatalf("same name by another user must be allowed: %v", err)
	}
	sf, err := s.GetSavedFilter(id)
	if err != nil || sf.Name != "Recent" || sf.UserID != 1 || sf.TableDefID != 1 {
		t.Fatalf("get = %+v err=%v", sf, err)
	}
	list, err := s.ListSavedFilters(1, 1)
	if err != nil || len(list) != 1 || list[0].Filters == "" {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if err := s.UpdateSavedFilter(id, "Renamed", `[]`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSavedFilter(1, 1, "Other", `[]`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSavedFilter(id, "Other", `[]`); err != ErrFilterTaken {
		t.Fatalf("rename into taken name err=%v", err)
	}
	if err := s.UpdateSavedFilter(id, "Recent", `[]`); err != nil {
		t.Fatalf("rename into a name held by another user must be allowed: %v", err)
	}
	if err := s.DeleteSavedFilter(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSavedFilter(id); err != ErrNotFound {
		t.Fatalf("get after delete err=%v", err)
	}
}
