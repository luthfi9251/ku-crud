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

func TestDatasourceDriver(t *testing.T) {
	s := openTest(t)
	d := &Datasource{Name: "pg1", Host: "h", Port: 1, DBName: "db", Username: "u",
		Password: "p", SSLMode: "disable", Driver: "mysql"}
	if err := s.CreateDatasource(d); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDatasource(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Driver != "mysql" {
		t.Fatalf("driver round-trip: %q", got.Driver)
	}
	// migration default: a row created without driver (raw SQL, simulating v1.2-pre data) reads as postgres
	if _, err := s.db.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode,raw)
		VALUES('legacy','h',1,'db','u','p','disable','')`); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetDatasource(d.ID + 1)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Driver != "postgres" {
		t.Fatalf("legacy driver default: %q", legacy.Driver)
	}
	got.Driver = "postgres"
	if err := s.UpdateDatasource(got); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetDatasource(got.ID)
	if after.Driver != "postgres" {
		t.Fatalf("update driver: %q", after.Driver)
	}
}
