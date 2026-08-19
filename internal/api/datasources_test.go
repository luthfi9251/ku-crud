package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestDatasourceEndpoints(t *testing.T) {
	s := newTestServer(t)
	c := login(s)

	w := do(s, "POST", "/api/datasources",
		`{"name":"prod","host":"localhost","port":5432,"dbname":"ku","username":"u","password":"secret","sslmode":"disable"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("password leaked in response")
	}

	// duplicate name → VALIDATION
	w = do(s, "POST", "/api/datasources",
		`{"name":"prod","host":"x","port":1,"dbname":"x","username":"x","password":"x","sslmode":"disable"}`, c)
	if w.Code != 400 {
		t.Fatalf("dup = %d %s", w.Code, w.Body)
	}

	// missing dbname → VALIDATION (dbname is mandatory)
	w = do(s, "POST", "/api/datasources",
		`{"name":"x","host":"x","port":1,"username":"x","password":"x","sslmode":"disable"}`, c)
	if w.Code != 400 {
		t.Fatalf("no dbname = %d %s", w.Code, w.Body)
	}

	w = do(s, "GET", "/api/datasources", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "prod") {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("password leaked in list")
	}

	// update: empty password keeps old
	w = do(s, "PUT", "/api/datasources/1",
		`{"name":"prod","host":"other","port":5432,"dbname":"ku","username":"u","password":"","sslmode":"disable"}`, c)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	got, _ := s.store.GetDatasource(1)
	if got.Password != "secret" || got.Host != "other" {
		t.Fatalf("update lost password/host: %+v", got)
	}

	// connection test against a dead local port → 502 CONN
	d := &meta.Datasource{Name: "dead", Host: "127.0.0.1", Port: 1, DBName: "x",
		Username: "x", Password: "x", SSLMode: "disable"}
	if err := s.store.CreateDatasource(d); err != nil {
		t.Fatal(err)
	}
	w = do(s, "POST", "/api/datasources/2/test", "", c)
	if w.Code != 502 || !strings.Contains(w.Body.String(), `"CONN"`) {
		t.Fatalf("test conn = %d %s", w.Code, w.Body)
	}
}
