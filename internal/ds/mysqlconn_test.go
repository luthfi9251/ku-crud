package ds

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func mysqlTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("KUCRUD_TEST_MYSQL")
	if dsn == "" {
		t.Skip("KUCRUD_TEST_MYSQL not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("no MySQL: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("no MySQL: %v", err)
	}
	return db
}

func TestMySQLConnect(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("select 1: %v %d", err, one)
	}
}
