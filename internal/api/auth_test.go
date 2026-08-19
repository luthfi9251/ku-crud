package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := meta.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateUser("alice", "secret"); err != nil {
		t.Fatal(err)
	}
	return New(s)
}

// do runs a request; when cookie ptr is non-nil it captures/sends the session cookie.
func do(s *Server, method, path, body string, cookie *string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil && *cookie != "" {
		req.Header.Set("Cookie", *cookie)
	}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if c := w.Header().Get("Set-Cookie"); c != "" && cookie != nil {
		*cookie = strings.SplitN(c, ";", 2)[0]
	}
	return w
}

func login(s *Server) *string {
	c := new(string)
	do(s, "POST", "/api/auth/login", `{"username":"alice","password":"secret"}`, c)
	return c
}

func TestLoginFlow(t *testing.T) {
	s := newTestServer(t)

	w := do(s, "POST", "/api/auth/login", `{"username":"alice","password":"wrong"}`, nil)
	if w.Code != 401 {
		t.Fatalf("bad login = %d", w.Code)
	}

	cookie := new(string)
	w = do(s, "POST", "/api/auth/login", `{"username":"alice","password":"secret"}`, cookie)
	if w.Code != 200 {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(*cookie, "ku_session=") {
		t.Fatal("no session cookie")
	}

	w = do(s, "GET", "/api/auth/me", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "alice") {
		t.Fatalf("me = %d %s", w.Code, w.Body)
	}

	w = do(s, "POST", "/api/auth/logout", "", cookie)
	if w.Code != 200 {
		t.Fatalf("logout = %d", w.Code)
	}
	w = do(s, "GET", "/api/auth/me", "", cookie)
	if w.Code != 401 {
		t.Fatalf("me after logout = %d", w.Code)
	}
}

func TestAuthRequired(t *testing.T) {
	s := newTestServer(t)
	for _, p := range []string{"/api/datasources", "/api/tables", "/api/audit"} {
		w := do(s, "GET", p, "", nil)
		if w.Code != 401 {
			t.Fatalf("%s unauthenticated = %d", p, w.Code)
		}
		var e map[string]any
		json.Unmarshal(w.Body.Bytes(), &e)
		if e["code"] != "AUTH" {
			t.Fatalf("%s code=%v", p, e["code"])
		}
	}
}

func TestTamperedCookie(t *testing.T) {
	s := newTestServer(t)
	cookie := new(string)
	do(s, "POST", "/api/auth/login", `{"username":"alice","password":"secret"}`, cookie)
	*cookie = "ku_session=alice|9999999999|deadbeef"
	if w := do(s, "GET", "/api/auth/me", "", cookie); w.Code != 401 {
		t.Fatalf("tampered cookie accepted: %d", w.Code)
	}
}
