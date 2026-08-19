package api

import (
	"database/sql"
	"strings"
	"testing"
)

func liveConn(t *testing.T, raw string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestVerifyAndResync(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)

	// no drift → ok
	w := do(s, "GET", "/api/tables/1/verify", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("verify = %d %s", w.Code, w.Body)
	}

	// simulate drift: rename column name → born2 (drops "born", adds "born2")
	db, _ := s.store.GetDatasource(2)
	conn := liveConn(t, db.Raw)
	if _, err := conn.Exec(`ALTER TABLE customers RENAME COLUMN born TO born2`); err != nil {
		t.Fatal(err)
	}

	w = do(s, "GET", "/api/tables/1/verify", "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"DRIFT"`) ||
		!strings.Contains(w.Body.String(), "born") {
		t.Fatalf("verify drift = %d %s", w.Code, w.Body)
	}

	// resync fixes it
	w = do(s, "POST", "/api/tables/1/resync", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "born2") {
		t.Fatalf("resync = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/1/verify", "", c)
	if w.Code != 200 {
		t.Fatalf("verify after resync = %d %s", w.Code, w.Body)
	}
}
