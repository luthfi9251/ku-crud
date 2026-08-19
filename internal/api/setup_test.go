package api

import (
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

// newTestServerNoUser is newTestServer without the seeded alice user.
func newTestServerNoUser(t *testing.T) *Server {
	t.Helper()
	store, err := meta.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	srv, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestSetupStatusNeeded(t *testing.T) {
	s := newTestServerNoUser(t)
	w := do(s, "GET", "/api/setup/status", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"needed":true`) {
		t.Fatalf("status = %d %s", w.Code, w.Body)
	}
}

func TestSetupCreateAndLockout(t *testing.T) {
	s := newTestServerNoUser(t)

	// create first user — no auth cookie at all
	w := do(s, "POST", "/api/setup", `{"username":"admin","password":"demo123"}`, nil)
	if w.Code != 200 {
		t.Fatalf("setup = %d %s", w.Code, w.Body)
	}

	// status flips
	w = do(s, "GET", "/api/setup/status", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"needed":false`) {
		t.Fatalf("status after = %d %s", w.Code, w.Body)
	}

	// can log in with the new credentials
	cookie := new(string)
	w = do(s, "POST", "/api/auth/login", `{"username":"admin","password":"demo123"}`, cookie)
	if w.Code != 200 {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}

	// second setup attempt is locked out — even with a valid session
	w = do(s, "POST", "/api/setup", `{"username":"evil","password":"hack123"}`, cookie)
	if w.Code != 403 {
		t.Fatalf("second setup = %d %s", w.Code, w.Body)
	}
}

func TestSetupValidation(t *testing.T) {
	s := newTestServerNoUser(t)
	for _, body := range []string{
		`{"username":"","password":"demo123"}`,
		`{"username":"admin","password":"abc"}`,
		`{"username":"admin"}`,
	} {
		if w := do(s, "POST", "/api/setup", body, nil); w.Code != 400 {
			t.Fatalf("body %s = %d %s", body, w.Code, w.Body)
		}
	}
}
