package api

import (
	"testing"
	"time"
)

func TestLoginLimiterWindow(t *testing.T) {
	base := time.Unix(1700000000, 0)
	cur := base
	l := newLoginLimiter(5, 15*time.Minute)
	l.now = func() time.Time { return cur }

	for i := 0; i < 5; i++ {
		if !l.allow("k") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
		l.fail("k")
	}
	if l.allow("k") {
		t.Fatal("6th attempt must be blocked")
	}
	// other keys unaffected
	if !l.allow("other") {
		t.Fatal("different key must be unaffected")
	}
	// success resets
	l.reset("k")
	if !l.allow("k") {
		t.Fatal("after reset must be allowed")
	}
	// failures expire with the window
	l.fail("k")
	cur = base.Add(16 * time.Minute)
	if !l.allow("k") {
		t.Fatal("expired failures must not block")
	}
}

func TestLoginRateLimitHTTP(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < 5; i++ {
		w := do(s, "POST", "/api/auth/login", `{"username":"alice","password":"wrong"}`, nil)
		if w.Code != 401 {
			t.Fatalf("attempt %d = %d %s", i+1, w.Code, w.Body)
		}
	}
	w := do(s, "POST", "/api/auth/login", `{"username":"alice","password":"wrong"}`, nil)
	if w.Code != 429 {
		t.Fatalf("6th attempt = %d %s", w.Code, w.Body)
	}
	// correct password is also blocked while throttled
	w = do(s, "POST", "/api/auth/login", `{"username":"alice","password":"secret"}`, nil)
	if w.Code != 429 {
		t.Fatalf("correct password while throttled = %d %s", w.Code, w.Body)
	}
}

func TestLoginSuccessResetsLimiter(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < 4; i++ {
		do(s, "POST", "/api/auth/login", `{"username":"alice","password":"wrong"}`, nil)
	}
	c := login(s) // 5th attempt with the right password: still allowed
	w := do(s, "GET", "/api/auth/me", "", c)
	if w.Code != 200 {
		t.Fatalf("me = %d %s", w.Code, w.Body)
	}
	// counter was reset by success: a full fresh window of failures is needed
	for i := 0; i < 5; i++ {
		do(s, "POST", "/api/auth/login", `{"username":"alice","password":"wrong"}`, nil)
	}
	if w = do(s, "POST", "/api/auth/login", `{"username":"alice","password":"wrong"}`, nil); w.Code != 429 {
		t.Fatalf("after 5 fresh failures = %d %s", w.Code, w.Body)
	}
}
