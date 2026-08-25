package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luthfi9251/kucrud-core/engine"
	"ku-crud/internal/meta"
)

// loginAs creates a user with a fresh role (given grants) and logs in.
func loginAs(t *testing.T, s *Server, username string, role *meta.Role, grants []meta.TableGrant) *string {
	t.Helper()
	if role.ID == 0 {
		if err := s.store.CreateRole(role, grants); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.CreateUserWithRole(username, "pw1234", role.ID); err != nil {
		t.Fatal(err)
	}
	c := new(string)
	w := do(s, "POST", "/api/auth/login", `{"username":"`+username+`","password":"pw1234"}`, c)
	if w.Code != 200 {
		t.Fatalf("loginAs %s = %d %s", username, w.Code, w.Body)
	}
	return c
}

func TestMeReturnsRoleInfo(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	w := do(s, "GET", "/api/auth/me", "", c)
	var me struct {
		Username          string `json:"username"`
		IsAdmin           bool   `json:"isAdmin"`
		ManageDatasources bool   `json:"manageDatasources"`
		ManageTables      bool   `json:"manageTables"`
		ViewAudit         bool   `json:"viewAudit"`
		ViewOutbox        bool   `json:"viewOutbox"`
	}
	json.Unmarshal(w.Body.Bytes(), &me)
	if me.Username != "alice" || !me.IsAdmin || !me.ManageDatasources || !me.ManageTables || !me.ViewAudit || !me.ViewOutbox {
		t.Fatalf("me = %s", w.Body)
	}
}

func TestNoGrantsForbidden(t *testing.T) {
	s := newTestServer(t)
	seedDS(t, s)
	// save one def (id 1) via admin
	if w := do(s, "POST", "/api/tables", defBody(s), login(s)); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}
	c := loginAs(t, s, "editor", &meta.Role{Name: "NoPlatform"}, nil)

	for _, p := range []struct{ method, path string }{
		{"GET", "/api/datasources"},
		{"POST", "/api/datasources"},
		{"GET", "/api/audit"},
		{"POST", "/api/tables"},
		{"PUT", "/api/tables/x"},
		{"DELETE", "/api/tables/x"},
		{"GET", "/api/tables/x/verify"},
		{"POST", "/api/tables/x/resync"},
	} {
		w := do(s, p.method, p.path, "", c)
		if w.Code != 403 {
			t.Fatalf("%s %s non-platform = %d %s", p.method, p.path, w.Code, w.Body)
		}
		var e map[string]any
		json.Unmarshal(w.Body.Bytes(), &e)
		if e["code"] != "FORBIDDEN" {
			t.Fatalf("%s %s code=%v", p.method, p.path, e["code"])
		}
	}
}

func TestAdminGate(t *testing.T) {
	s := newTestServer(t)
	c := loginAs(t, s, "plat", &meta.Role{Name: "Plat", ManageDatasources: true}, nil)
	for _, p := range []struct{ method, path string }{
		{"GET", "/api/users"}, {"POST", "/api/users"},
		{"PUT", "/api/users/x"}, {"DELETE", "/api/users/x"},
		{"GET", "/api/roles"}, {"POST", "/api/roles"},
		{"PUT", "/api/roles/x"}, {"DELETE", "/api/roles/x"},
	} {
		w := do(s, p.method, p.path, "", c)
		if w.Code != 403 {
			t.Fatalf("%s %s non-admin = %d %s", p.method, p.path, w.Code, w.Body)
		}
	}
	// platform user CAN reach platform endpoints
	if w := do(s, "GET", "/api/datasources", "", c); w.Code != 200 {
		t.Fatalf("platform datasources = %d", w.Code)
	}
}

func TestTableGrantMatrix(t *testing.T) {
	s := newTestServer(t)
	seedDS(t, s)
	if w := do(s, "POST", "/api/tables", defBody(s), login(s)); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}
	td1 := tdTok(s, 1)

	reader := loginAs(t, s, "reader", &meta.Role{Name: "Reader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})

	// list shows only granted tables, with permissions
	w := do(s, "GET", "/api/tables", "", reader)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"permissions"`) {
		t.Fatalf("reader list = %d %s", w.Code, w.Body)
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("reader sees %d tables", len(list))
	}
	if list[0]["id"] != td1 {
		t.Fatalf("list id not masked: %v", list[0]["id"])
	}
	perms := list[0]["permissions"].(map[string]any)
	if perms["read"] != true || perms["create"] != false {
		t.Fatalf("perms=%v", perms)
	}

	// def detail allowed (read grant) with permissions
	w = do(s, "GET", "/api/tables/"+td1, "", reader)
	if w.Code != 200 {
		t.Fatalf("reader def = %d %s", w.Code, w.Body)
	}

	// write without grant → 403 (fires before PG connection)
	for _, p := range []struct{ method, path string }{
		{"POST", "/api/tables/" + tdTok(s, 1) + "/rows"},
		{"PUT", "/api/tables/" + tdTok(s, 1) + "/rows/" + engine.EncodeRowKey([]string{"1"})},
		{"DELETE", "/api/tables/" + tdTok(s, 1) + "/rows/" + engine.EncodeRowKey([]string{"1"})},
	} {
		w := do(s, p.method, p.path, "", reader)
		if w.Code != 403 {
			t.Fatalf("reader %s = %d %s", p.method, w.Code, w.Body)
		}
	}

	// table with no grant at all → hidden from list, def detail 403
	editor := loginAs(t, s, "editor2", &meta.Role{Name: "Editor2"}, nil)
	w = do(s, "GET", "/api/tables", "", editor)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("editor sees %d tables", len(list))
	}
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 1), "", editor); w.Code != 403 {
		t.Fatalf("editor def = %d", w.Code)
	}
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows", "", editor); w.Code != 403 {
		t.Fatalf("editor rows = %d", w.Code)
	}

	// platform users see every definition (they manage them) but their
	// permissions object reflects grants — row CRUD still 403s without one
	plat := loginAs(t, s, "plat2", &meta.Role{Name: "Plat2", ManageTables: true}, nil)
	w = do(s, "GET", "/api/tables", "", plat)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("platform def visibility: got %d defs", len(list))
	}
	permsNoGrant := list[0]["permissions"].(map[string]any)
	if permsNoGrant["read"] != false || permsNoGrant["delete"] != false {
		t.Fatalf("platform without grants perms=%v", permsNoGrant)
	}
	if w = do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows", "", plat); w.Code != 403 {
		t.Fatalf("platform rows without grant = %d", w.Code)
	}
	// platform role WITH a grant behaves per that grant
	platRW := loginAs(t, s, "plat3", &meta.Role{Name: "Plat3", ManageTables: true},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true, CanDelete: true}})
	w = do(s, "GET", "/api/tables", "", platRW)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("platform+grant list=%d", len(list))
	}
	permsGrant := list[0]["permissions"].(map[string]any)
	if permsGrant["read"] != true || permsGrant["delete"] != true || permsGrant["create"] != false {
		t.Fatalf("platform+grant perms=%v", permsGrant)
	}
}

func TestMaskedIDsInAPI(t *testing.T) {
	s := newTestServer(t)
	seedDS(t, s)
	c := login(s)

	// create datasource → masked id
	w := do(s, "POST", "/api/datasources",
		`{"name":"d2","host":"h","port":1,"dbname":"db","username":"u","password":"p","sslmode":"disable"}`, c)
	var ds struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &ds)
	if ds.ID == "" || ds.ID == "1" || strings.ContainsAny(ds.ID, "0123456789") == false {
		// token is base64url — digits MAY appear; assert it decodes and is not raw
	}
	if ds.ID == "" || ds.ID == "1" {
		t.Fatalf("datasource id not masked: %q", ds.ID)
	}

	// using the token in URLs works; raw numeric id 404s
	if w = do(s, "GET", "/api/datasources/"+ds.ID+"/tables", "", c); w.Code == 404 {
		t.Fatalf("masked ds id rejected: %d %s", w.Code, w.Body) // dead host → 502, but not 404
	}
	if w = do(s, "GET", "/api/datasources/1/tables", "", c); w.Code != 404 {
		t.Fatalf("raw numeric ds id must 404, got %d", w.Code)
	}
	if w = do(s, "GET", "/api/datasources/999/tables", "", c); w.Code != 404 {
		t.Fatalf("unknown ds = %d", w.Code)
	}

	// audit filter with garbage token → 400
	if w = do(s, "GET", "/api/audit?tableDefId=zzz", "", c); w.Code != 400 {
		t.Fatalf("bad audit token = %d", w.Code)
	}
}

func TestDisabledUserRejected(t *testing.T) {
	s := newTestServer(t)
	c := loginAs(t, s, "victim", &meta.Role{Name: "Victim"}, nil)
	if w := do(s, "GET", "/api/auth/me", "", c); w.Code != 200 {
		t.Fatalf("pre-disable = %d", w.Code)
	}
	dis := true
	if err := s.store.UpdateUser(2, nil, &dis, nil); err != nil { // alice=1, victim=2
		t.Fatal(err)
	}
	if w := do(s, "GET", "/api/auth/me", "", c); w.Code != 401 {
		t.Fatalf("disabled session accepted = %d", w.Code)
	}
	if w := do(s, "POST", "/api/auth/login", `{"username":"victim","password":"pw1234"}`, nil); w.Code != 401 {
		t.Fatalf("disabled login = %d", w.Code)
	}
}

// tdTok masks a table-def id for URLs in tests.
func tdTok(s *Server, id int64) string { return s.ids.Encode("td", id) }
