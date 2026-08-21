package api

import (
	"encoding/json"
	"testing"
)

func TestMeLanguage(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	var me map[string]any
	w := do(s, "GET", "/api/auth/me", "", c)
	json.Unmarshal(w.Body.Bytes(), &me)
	if me["language"] != "en" {
		t.Fatalf("default language = %v", me["language"])
	}
	w = do(s, "PATCH", "/api/auth/me", `{"language":"id"}`, c)
	if w.Code != 200 {
		t.Fatalf("patch = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/auth/me", "", c)
	json.Unmarshal(w.Body.Bytes(), &me)
	if me["language"] != "id" {
		t.Fatalf("language after patch = %v", me["language"])
	}
	w = do(s, "PATCH", "/api/auth/me", `{"language":"xx"}`, c)
	if w.Code != 400 {
		t.Fatalf("bad language = %d %s", w.Code, w.Body)
	}
}
