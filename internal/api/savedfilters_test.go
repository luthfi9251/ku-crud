package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestSavedFiltersAPI(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedDS(t, s)
	do(s, "POST", "/api/tables", defBody(s), c) // ensure a def exists (id 1)
	tok := tdTok(s, 1)

	w := do(s, "GET", "/api/tables/"+tok+"/saved-filters", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}

	w = do(s, "POST", "/api/tables/"+tok+"/saved-filters",
		`{"name":"Open","filters":"[{\"column\":\"status\",\"op\":\"eq\",\"values\":[\"open\"]}]"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var got struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.ID == "" || got.ID == "1" {
		t.Fatalf("saved filter id not masked: %q", got.ID)
	}

	// bad filter payload (unknown column) rejected
	w = do(s, "POST", "/api/tables/"+tok+"/saved-filters",
		`{"name":"Bad","filters":"[{\"column\":\"nope\",\"op\":\"eq\",\"values\":[\"x\"]}]"}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "FILTER_INVALID") {
		t.Fatalf("bad filter not rejected: %d %s", w.Code, w.Body)
	}

	// duplicate name → 409
	w = do(s, "POST", "/api/tables/"+tok+"/saved-filters",
		`{"name":"Open","filters":"[]"}`, c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "FILTER_NAME_TAKEN") {
		t.Fatalf("duplicate = %d %s", w.Code, w.Body)
	}

	w = do(s, "PUT", "/api/tables/"+tok+"/saved-filters/"+got.ID, `{"name":"Open (edit)","filters":"[]"}`, c)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}

	// another user cannot see or touch it
	c2 := loginAs(t, s, "bob", &meta.Role{Name: "viewer"}, []meta.TableGrant{{TableDefID: 1, CanRead: true}})
	w = do(s, "GET", "/api/tables/"+tok+"/saved-filters", "", c2)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Open (edit)") {
		t.Fatalf("bob sees alice filter: %d %s", w.Code, w.Body)
	}
	w = do(s, "DELETE", "/api/tables/"+tok+"/saved-filters/"+got.ID, "", c2)
	if w.Code != 404 {
		t.Fatalf("bob delete alice filter = %d (want 404)", w.Code)
	}

	w = do(s, "DELETE", "/api/tables/"+tok+"/saved-filters/"+got.ID, "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
}
