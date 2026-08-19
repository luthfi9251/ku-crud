package ds

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type DSN struct {
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
	SSLMode  string
	Raw      string // full conn string passthrough (tests); bypasses other fields
}

func (d DSN) ConnString() string {
	if d.Raw != "" {
		return d.Raw
	}
	ssl := d.SSLMode
	if ssl == "" {
		ssl = "disable"
	}
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		d.Host, d.Port, d.DBName, d.Username, d.Password, ssl)
}

func Connect(d DSN) (*sql.DB, error) {
	db, err := sql.Open("pgx", d.ConnString())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
