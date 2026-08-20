package api

import (
	"encoding/json"
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
	w = do(s, "PUT", "/api/datasources/"+s.ids.Encode("ds", 1),
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
	w = do(s, "POST", "/api/datasources/"+s.ids.Encode("ds", 2)+"/test", "", c)
	if w.Code != 502 || !strings.Contains(w.Body.String(), `"CONN"`) {
		t.Fatalf("test conn = %d %s", w.Code, w.Body)
	}
}

func TestDatasourceDriverField(t *testing.T) {
	s := newTestServer(t)
	c := login(s)

	// create mysql datasource (dead host fine — validation only)
	body := `{"name":"my","driver":"mysql","host":"h","port":3306,"dbname":"db",
		"username":"u","password":"p","sslmode":"disable"}`
	w := do(s, "POST", "/api/datasources", body, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"driver":"mysql"`) {
		t.Fatalf("create mysql ds = %d %s", w.Code, w.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)

	// missing driver → defaults postgres
	w = do(s, "POST", "/api/datasources",
		`{"name":"pg","host":"h","port":5432,"dbname":"db","username":"u","password":"p","sslmode":"disable"}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"driver":"postgres"`) {
		t.Fatalf("default driver = %d %s", w.Code, w.Body)
	}

	// unknown driver → 400
	w = do(s, "POST", "/api/datasources",
		`{"name":"x","driver":"oracle","host":"h","port":1,"dbname":"db","username":"u","password":"p","sslmode":"disable"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "unsupported driver") {
		t.Fatalf("oracle = %d %s", w.Code, w.Body)
	}

	// update keeps driver field
	w = do(s, "PUT", "/api/datasources/"+created.ID,
		`{"name":"my2","driver":"mysql","host":"h","port":3306,"dbname":"db","username":"u","password":"p","sslmode":"disable"}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"driver":"mysql"`) {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
}
