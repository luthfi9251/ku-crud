package meta

import (
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.path = path
	return s
}

func TestMigrationsAndSettings(t *testing.T) {
	s := openTest(t)
	if v, ok, _ := s.Setting("schema_version"); !ok || v == "" {
		t.Fatal("schema_version not set after Open")
	}
	if err := s.SetSetting("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.Setting("foo"); !ok || v != "bar" {
		t.Fatalf("got %q %v", v, ok)
	}
	// reopening applies no duplicate migrations and keeps data
	path := s.path
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, ok, _ := s2.Setting("foo"); !ok || v != "bar" {
		t.Fatal("settings lost after reopen")
	}
}

func TestUsers(t *testing.T) {
	s := openTest(t)
	if err := s.CreateUser("alice", "secret"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Fatalf("users=%d", n)
	}
	if ok, _ := s.VerifyUser("alice", "secret"); !ok {
		t.Fatal("correct password rejected")
	}
	if ok, _ := s.VerifyUser("alice", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if ok, _ := s.VerifyUser("bob", "secret"); ok {
		t.Fatal("unknown user accepted")
	}
	if id, ok, _ := s.UserID("alice"); !ok || id != 1 {
		t.Fatalf("UserID=%d %v", id, ok)
	}
}
