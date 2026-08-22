package meta

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

// buildV10DB simulates an existing v1.0 instance: migrations 1-2 applied,
// one user and one table def with the old pk_column shape.
func buildV10DB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v10.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES('schema_version','2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(username,password_hash) VALUES('old','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,pk_column,page_size)
		VALUES(1,'public','t','T','code',20)`); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigration3Upgrade(t *testing.T) {
	path := buildV10DB(t)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// existing user became Admin and is marked first
	u, ok, err := s.GetUserContext("old")
	if err != nil || !ok {
		t.Fatalf("GetUserContext: %v %v", ok, err)
	}
	if !u.IsAdmin || !u.ManageDatasources || !u.ManageTables || !u.ViewAudit || !u.ViewOutbox {
		t.Fatalf("v1.0 user should be Admin: %+v", u)
	}
	if !u.IsFirst {
		t.Fatal("v1.0 user should be marked is_first")
	}

	// pk_column was migrated into key_columns JSON, old column dropped
	var kc string
	if err := s.db.QueryRow(`SELECT key_columns FROM table_defs`).Scan(&kc); err != nil {
		t.Fatalf("key_columns: %v", err)
	}
	var keys []string
	if err := json.Unmarshal([]byte(kc), &keys); err != nil || len(keys) != 1 || keys[0] != "code" {
		t.Fatalf("key_columns=%q err=%v", kc, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('table_defs')
		WHERE name='pk_column'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("pk_column should be dropped")
	}

	// Admin role exists with id 1
	var name string
	if err := s.db.QueryRow(`SELECT name FROM roles WHERE id=1`).Scan(&name); err != nil || name != "Admin" {
		t.Fatalf("admin role: %q %v", name, err)
	}
}

func TestIDSecretStable(t *testing.T) {
	s := openTest(t)
	a, err := s.IDSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 { // 32 bytes hex
		t.Fatalf("secret len %d", len(a))
	}
	s.Close()
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	b, _ := s2.IDSecret()
	if string(a) != string(b) {
		t.Fatal("id_secret not stable across reopen")
	}
}
