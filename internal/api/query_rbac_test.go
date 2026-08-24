package api

import (
	"encoding/json"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

// Query defs under RBAC: a read grant surfaces them like any table (with the
// non-admin perm lock zeroing create/update/delete), no-grant users are
// locked out before any live connection is attempted, and the QUERY_READONLY
// guard precedes the perm check on writes. The datasource is "dead"
// (unreachable) so any assertion that passes the perm wall would fail loudly
// at connect — the 403s below are deterministic, not incidental.
func TestQueryDefGrantMatrix(t *testing.T) {
	s := newTestServer(t)
	seedQueryDef(t, s, []string{"n"}) // ds "dead" id 1 + query def id 1
	tok := tdTok(s, 1)

	reader := loginAs(t, s, "qreader", &meta.Role{Name: "QReader"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true}})
	stranger := loginAs(t, s, "qstranger", &meta.Role{Name: "QStranger"}, nil)

	// read grant: list includes the query def; detail 200 with permissions
	// read=true and create/update/delete=false (perm lock for non-admins)
	w := do(s, "GET", "/api/tables", "", reader)
	if w.Code != 200 {
		t.Fatalf("reader list = %d %s", w.Code, w.Body)
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["id"] != tok {
		t.Fatalf("reader sees %s", w.Body)
	}
	w = do(s, "GET", "/api/tables/"+tok, "", reader)
	if w.Code != 200 {
		t.Fatalf("reader def = %d %s", w.Code, w.Body)
	}
	var dto map[string]any
	json.Unmarshal(w.Body.Bytes(), &dto)
	perms := dto["permissions"].(map[string]any)
	if perms["read"] != true || perms["create"] != false ||
		perms["update"] != false || perms["delete"] != false {
		t.Fatalf("reader perms = %v", perms)
	}

	// no grant: excluded from list, detail 403, rows 403 — the row-list perm
	// check fires before liveAdapter, so no connection is ever attempted
	w = do(s, "GET", "/api/tables", "", stranger)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("stranger sees %d tables", len(list))
	}
	if w = do(s, "GET", "/api/tables/"+tok, "", stranger); w.Code != 403 ||
		!strings.Contains(w.Body.String(), "FORBIDDEN") {
		t.Fatalf("stranger def = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", "/api/tables/"+tok+"/rows", "", stranger); w.Code != 403 ||
		!strings.Contains(w.Body.String(), "FORBIDDEN") {
		t.Fatalf("stranger rows = %d %s", w.Code, w.Body)
	}

	// precedence: the QUERY_READONLY guard fires before the perm check on
	// writes — structural behavior pinned
	if w = do(s, "POST", "/api/tables/"+tok+"/rows", "{}", stranger); w.Code != 403 ||
		!strings.Contains(w.Body.String(), "QUERY_READONLY") {
		t.Fatalf("stranger write = %d %s", w.Code, w.Body)
	}

	// keyed def: no-grant row-get hits the perm wall (403 FORBIDDEN)
	pk := rowKeyToken([]string{"jo"})
	if w = do(s, "GET", "/api/tables/"+tok+"/rows/"+pk, "", stranger); w.Code != 403 ||
		!strings.Contains(w.Body.String(), "FORBIDDEN") {
		t.Fatalf("stranger row get = %d %s", w.Code, w.Body)
	}
}

// No-key query def: a no-grant user must hit the perm check, not QUERY_NO_KEY
// (perm runs first — no probing key-column presence).
func TestQueryDefNoKeyNoGrantPermFirst(t *testing.T) {
	s := newTestServer(t)
	seedQueryDef(t, s, nil) // query def without key columns
	stranger := loginAs(t, s, "qstranger2", &meta.Role{Name: "QStranger2"}, nil)
	w := do(s, "GET", "/api/tables/"+tdTok(s, 1)+"/rows/anything", "", stranger)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "FORBIDDEN") {
		t.Fatalf("no-grant no-key row get = %d %s", w.Code, w.Body)
	}
}
