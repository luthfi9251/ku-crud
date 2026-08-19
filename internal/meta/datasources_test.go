package meta

import "testing"

func TestDatasourceCRUD(t *testing.T) {
	s := openTest(t)
	d := &Datasource{Name: "prod", Host: "db.example", Port: 5432, DBName: "app",
		Username: "u", Password: "p", SSLMode: "require"}
	if err := s.CreateDatasource(d); err != nil {
		t.Fatal(err)
	}
	if d.ID == 0 {
		t.Fatal("id not set")
	}
	got, _ := s.GetDatasource(d.ID)
	if got.Password != "p" || got.DBName != "app" {
		t.Fatalf("got %+v", got)
	}
	got.Host = "newhost"
	if err := s.UpdateDatasource(got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetDatasource(d.ID)
	if got2.Host != "newhost" {
		t.Fatal("update lost")
	}
	if list, _ := s.ListDatasources(); len(list) != 1 {
		t.Fatalf("list=%d", len(list))
	}
	if err := s.DeleteDatasource(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDatasource(d.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
