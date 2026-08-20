package meta

import (
	"strings"
	"testing"
)

func TestPasswordEncryptionRoundtrip(t *testing.T) {
	s := openTest(t)
	defer s.Close()

	enc, err := encryptPassword(s, "s3cret päss")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "v1:") {
		t.Fatalf("ciphertext prefix: %q", enc)
	}
	if strings.Contains(enc, "s3cret") {
		t.Fatal("ciphertext leaks plaintext")
	}
	dec, err := decryptPassword(s, enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != "s3cret päss" {
		t.Fatalf("roundtrip: %q", dec)
	}
}

func TestDecryptLegacyPlaintextPassthrough(t *testing.T) {
	s := openTest(t)
	defer s.Close()
	got, err := decryptPassword(s, "plain-old-password")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain-old-password" {
		t.Fatalf("legacy passthrough: %q", got)
	}
}

func TestCryptKeyStable(t *testing.T) {
	s := openTest(t)
	a, err := s.cryptKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 {
		t.Fatalf("key len %d", len(a))
	}
	s.Close()
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	b, _ := s2.cryptKey()
	if string(a) != string(b) {
		t.Fatal("crypt key not stable across reopen")
	}
}

func TestDatasourcePasswordStoredEncrypted(t *testing.T) {
	s := openTest(t)
	defer s.Close()
	d := &Datasource{Name: "pg", Host: "h", Port: 5432, DBName: "db", Username: "u",
		Password: "hunter2", SSLMode: "disable", Driver: "postgres"}
	if err := s.CreateDatasource(d); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT password FROM datasources WHERE id=?`, d.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "v1:") || strings.Contains(stored, "hunter2") {
		t.Fatalf("stored password not encrypted: %q", stored)
	}
	got, err := s.GetDatasource(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "hunter2" {
		t.Fatalf("decrypted password: %q", got.Password)
	}
	// update keeps the same transparency
	got.Password = "newpass"
	if err := s.UpdateDatasource(got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetDatasource(d.ID)
	if got2.Password != "newpass" {
		t.Fatalf("updated password: %q", got2.Password)
	}
	list, err := s.ListDatasources()
	if err != nil || len(list) != 1 || list[0].Password != "newpass" {
		t.Fatalf("list: %v %+v", err, list)
	}
}

func TestEncryptLegacyPasswordsMigrationPass(t *testing.T) {
	s := openTest(t)
	// raw-insert a legacy plaintext row (bypassing the encrypting writer)
	if _, err := s.db.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode,driver,raw)
		VALUES('legacy','h',1,'db','u','oldplain','disable','postgres','')`); err != nil {
		t.Fatal(err)
	}
	if err := s.encryptLegacyPasswords(); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT password FROM datasources WHERE name='legacy'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "v1:") {
		t.Fatalf("legacy row not encrypted: %q", stored)
	}
	got, err := s.GetDatasource(1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "oldplain" {
		t.Fatalf("legacy decrypt: %q", got.Password)
	}
	// idempotent: running again does not double-encrypt
	if err := s.encryptLegacyPasswords(); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetDatasource(1)
	if got2.Password != "oldplain" {
		t.Fatalf("idempotent: %q", got2.Password)
	}
}
