package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestGroupsAPI(t *testing.T) {
	s := newTestServer(t)
	c := login(s)

	w := do(s, "POST", "/api/groups", `{"name":"Sales"}`, c)
	if w.Code != 200 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var g struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &g)

	w = do(s, "GET", "/api/groups", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Sales"`) {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}

	w = do(s, "POST", "/api/groups", `{"name":"Sales"}`, c)
	if w.Code != 409 {
		t.Fatalf("dup = %d %s", w.Code, w.Body)
	}

	w = do(s, "PATCH", "/api/groups/"+g.ID, `{"name":"Revenue"}`, c)
	if w.Code != 200 {
		t.Fatalf("rename = %d %s", w.Code, w.Body)
	}

	seedDS(t, s)
	w = do(s, "POST", "/api/tables", defBody(s), c)
	var td struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &td)

	w = do(s, "PATCH", "/api/tables/"+td.ID, `{"groupId":"`+g.ID+`"}`, c)
	if w.Code != 200 {
		t.Fatalf("assign = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/"+td.ID, "", c)
	if !strings.Contains(w.Body.String(), `"groupName":"Revenue"`) {
		t.Fatalf("groupName missing: %s", w.Body)
	}

	w = do(s, "DELETE", "/api/groups/"+g.ID, "", c)
	if w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/tables/"+td.ID, "", c)
	if strings.Contains(w.Body.String(), `"groupName"`) {
		t.Fatalf("table should be ungrouped after group delete: %s", w.Body)
	}
}

func TestGroupsNonPMForbidden(t *testing.T) {
	s := newTestServer(t)
	c2 := loginAs(t, s, "bob", &meta.Role{Name: "viewer"}, nil)
	w := do(s, "POST", "/api/groups", `{"name":"X"}`, c2)
	if w.Code != 403 {
		t.Fatalf("non-PM create = %d (want 403): %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/groups", "", c2)
	if w.Code != 200 {
		t.Fatalf("read should be allowed for all authed: %d", w.Code)
	}
}
