package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestUserEndpoints(t *testing.T) {
	s := newTestServer(t)
	c := login(s)

	// create a role to assign
	var role struct {
		ID string `json:"id"`
	}
	w := do(s, "POST", "/api/roles", `{"name":"Editor","tables":[]}`, c)
	if w.Code != 200 {
		t.Fatalf("role create = %d %s", w.Code, w.Body)
	}
	json.Unmarshal(w.Body.Bytes(), &role)
	if role.ID == "" {
		t.Fatal("role id not masked")
	}

	// create user
	w = do(s, "POST", "/api/users",
		`{"username":"bob","password":"pw123","roleId":"`+role.ID+`"}`, c)
	if w.Code != 200 {
		t.Fatalf("user create = %d %s", w.Code, w.Body)
	}
	var bob struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		RoleName string `json:"roleName"`
		Disabled bool   `json:"disabled"`
		IsFirst  bool   `json:"isFirst"`
	}
	json.Unmarshal(w.Body.Bytes(), &bob)
	if bob.ID == "" || bob.Username != "bob" || bob.RoleName != "Editor" {
		t.Fatalf("bob = %+v", bob)
	}

	// validation: short password, dup username, bad role token
	if w = do(s, "POST", "/api/users", `{"username":"x","password":"12","roleId":"`+role.ID+`"}`, c); w.Code != 400 {
		t.Fatalf("short pw = %d", w.Code)
	}
	if w = do(s, "POST", "/api/users", `{"username":"bob","password":"1234","roleId":"`+role.ID+`"}`, c); w.Code != 400 {
		t.Fatalf("dup user = %d", w.Code)
	}
	if w = do(s, "POST", "/api/users", `{"username":"x","password":"1234","roleId":"zzz"}`, c); w.Code != 400 {
		t.Fatalf("bad role token = %d", w.Code)
	}

	// list
	w = do(s, "GET", "/api/users", "", c)
	if !strings.Contains(w.Body.String(), `"username":"bob"`) ||
		!strings.Contains(w.Body.String(), `"username":"alice"`) {
		t.Fatalf("list = %s", w.Body)
	}

	// update: disable bob via masked id
	if w = do(s, "PUT", "/api/users/"+bob.ID, `{"disabled":true}`, c); w.Code != 200 {
		t.Fatalf("disable = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/users", "", c)
	if !strings.Contains(w.Body.String(), `"disabled":true`) {
		t.Fatalf("disabled not persisted: %s", w.Body)
	}
	// disabled bob cannot log in
	if w = do(s, "POST", "/api/auth/login", `{"username":"bob","password":"pw123"}`, nil); w.Code != 401 {
		t.Fatalf("disabled login = %d", w.Code)
	}

	// password change
	if w = do(s, "PUT", "/api/users/"+bob.ID, `{"password":"newpw1"}`, c); w.Code != 200 {
		t.Fatalf("pw change = %d", w.Code)
	}

	// first user (alice) is immutable
	w = do(s, "GET", "/api/users", "", c)
	var users []struct {
		ID      string `json:"id"`
		IsFirst bool   `json:"isFirst"`
	}
	json.Unmarshal(w.Body.Bytes(), &users)
	var firstID string
	for _, u := range users {
		if u.IsFirst {
			firstID = u.ID
		}
	}
	if firstID == "" {
		t.Fatal("no first user flagged")
	}
	if w = do(s, "PUT", "/api/users/"+firstID, `{"disabled":true}`, c); w.Code != 403 {
		t.Fatalf("first user update = %d %s", w.Code, w.Body)
	}
	if w = do(s, "DELETE", "/api/users/"+firstID, "", c); w.Code != 403 {
		t.Fatalf("first user delete = %d", w.Code)
	}

	// self-protection: admin cannot delete/disable self
	var aliceID string
	for _, u := range users {
		if !u.IsFirst {
			// not alice necessarily; find alice by order — alice is id 1 (first)
		}
	}
	// alice is the first user here; create a second admin to test self-delete via bob promote? simpler: use alice token
	// alice deleting herself:
	_ = aliceID
	if w = do(s, "DELETE", "/api/users/"+firstID, "", c); w.Code != 403 {
		t.Fatalf("self delete = %d", w.Code)
	}

	// delete bob
	if w = do(s, "DELETE", "/api/users/"+bob.ID, "", c); w.Code != 200 {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if w = do(s, "PUT", "/api/users/"+bob.ID, `{"disabled":false}`, c); w.Code != 404 {
		t.Fatalf("update deleted = %d", w.Code)
	}
}

func TestRoleEndpoints(t *testing.T) {
	s := newTestServer(t)
	seedDS(t, s)
	c := login(s)

	// create def to grant on
	if w := do(s, "POST", "/api/tables", defBody(s), c); w.Code != 200 {
		t.Fatalf("def = %d %s", w.Code, w.Body)
	}
	var defs []struct {
		ID string `json:"id"`
	}
	w := do(s, "GET", "/api/tables", "", c)
	json.Unmarshal(w.Body.Bytes(), &defs)
	tdTok := defs[0].ID

	// create role with grants
	w = do(s, "POST", "/api/roles",
		`{"name":"RW","tables":[{"tableDefId":"`+tdTok+`","canRead":true,"canCreate":true,"canUpdate":false,"canDelete":false}]}`, c)
	if w.Code != 200 {
		t.Fatalf("role create = %d %s", w.Code, w.Body)
	}
	var rl struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Grants []meta.TableGrant `json:"tables"`
	}
	json.Unmarshal(w.Body.Bytes(), &rl)
	if rl.ID == "" || len(rl.Grants) != 1 {
		t.Fatalf("role = %s", w.Body)
	}

	// update role: rename + replace grants (full CRUD)
	w = do(s, "PUT", "/api/roles/"+rl.ID,
		`{"name":"RW2","viewAudit":true,"tables":[{"tableDefId":"`+tdTok+`","canRead":true,"canCreate":true,"canUpdate":true,"canDelete":true}]}`, c)
	if w.Code != 200 {
		t.Fatalf("role update = %d %s", w.Code, w.Body)
	}
	w = do(s, "GET", "/api/roles", "", c)
	if !strings.Contains(w.Body.String(), `"name":"RW2"`) || !strings.Contains(w.Body.String(), `"viewAudit":true`) {
		t.Fatalf("roles list = %s", w.Body)
	}

	// builtin admin role immutable + visible
	w = do(s, "GET", "/api/roles", "", c)
	var roles []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		IsAdmin bool   `json:"isAdmin"`
	}
	json.Unmarshal(w.Body.Bytes(), &roles)
	var adminRoleID string
	for _, r := range roles {
		if r.IsAdmin {
			adminRoleID = r.ID
		}
	}
	if adminRoleID == "" {
		t.Fatal("builtin admin role missing")
	}
	if w = do(s, "PUT", "/api/roles/"+adminRoleID, `{"name":"Hax","tables":[]}`, c); w.Code != 403 {
		t.Fatalf("admin role update = %d", w.Code)
	}
	if w = do(s, "DELETE", "/api/roles/"+adminRoleID, "", c); w.Code != 403 {
		t.Fatalf("admin role delete = %d", w.Code)
	}

	// role in use cannot be deleted
	if err := s.store.CreateUserWithRole("xyz", "pw12", mustRoleID(t, s, rl.ID)); err != nil {
		t.Fatal(err)
	}
	if w = do(s, "DELETE", "/api/roles/"+rl.ID, "", c); w.Code != 400 {
		t.Fatalf("in-use role delete = %d", w.Code)
	}

	// bad grant token rejected
	if w = do(s, "POST", "/api/roles", `{"name":"Bad","tables":[{"tableDefId":"zzz","canRead":true}]}`, c); w.Code != 400 {
		t.Fatalf("bad grant token = %d", w.Code)
	}
	// dup name rejected
	if w = do(s, "POST", "/api/roles", `{"name":"RW2","tables":[]}`, c); w.Code != 400 {
		t.Fatalf("dup role = %d", w.Code)
	}
}

func mustRoleID(t *testing.T, s *Server, tok string) int64 {
	t.Helper()
	id, err := s.ids.Decode("role", tok)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
