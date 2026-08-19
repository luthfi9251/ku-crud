package ds

import "testing"

func TestConnStringQuoting(t *testing.T) {
	d := DSN{Host: "db host", Port: 5432, DBName: "my db", Username: "u ser",
		Password: "my secret 'pass' \\", SSLMode: "require"}
	want := `host='db host' port=5432 dbname='my db' user='u ser' password='my secret \'pass\' \\' sslmode='require'`
	if got := d.ConnString(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := (DSN{Port: 1, Raw: "KUCRUD_TEST_PG=x"}).ConnString(); got != "KUCRUD_TEST_PG=x" {
		t.Fatalf("raw passthrough broken: %s", got)
	}
}
