package api

import (
	"fmt"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

type grants struct{ ds, tables, audit, outbox bool }

func roleWith(name string, g grants) *meta.Role {
	return &meta.Role{Name: name, ManageDatasources: g.ds, ManageTables: g.tables,
		ViewAudit: g.audit, ViewOutbox: g.outbox}
}

// one probe endpoint per gate (+ meta export which needs both ds+tables)
var gateProbes = []string{
	"/api/datasources",  // RequireDSManage
	"/api/tables!POST",  // RequireTablesManage
	"/api/audit",        // RequireAuditView
	"/api/hooks/outbox", // RequireOutboxView
	"/api/meta/export",  // RequirePlatformAll (ds AND tables)
	"/api/hooks",        // RequireTablesManage (definition editor dropdown)
}

// probePostSeq uniquifies each probe's table name: gate tests POST the
// def body repeatedly and expect 200, but table names are a globally
// unique namespace (TestTableDefDuplicateNameRejected).
var probePostSeq int

func probeStatus(t *testing.T, s *Server, cookie *string, probe string) int {
	t.Helper()
	method, path := "GET", probe
	if i := len(probe) - len("!POST"); i > 0 && probe[i:] == "!POST" {
		method, path = "POST", probe[:i]
	}
	body := ""
	if method == "POST" {
		probePostSeq++
		body = strings.Replace(defBody(s), `"tableName":"customers"`,
			fmt.Sprintf(`"tableName":"customers%d"`, probePostSeq), 1)
	}
	return do(s, method, path, body, cookie).Code
}

func TestSplitGrantGates(t *testing.T) {
	s := newTestServer(t)
	seedDS(t, s)
	admin := login(s)

	// admin passes everything
	for _, p := range gateProbes {
		if c := probeStatus(t, s, admin, p); c == 403 || c == 401 {
			t.Fatalf("admin probe %s = %d", p, c)
		}
	}

	// seed one def via admin so later tasks have a target
	if w := do(s, "POST", "/api/tables", defBody(s), login(s)); w.Code != 200 {
		t.Fatalf("create def = %d %s", w.Code, w.Body)
	}

	cases := []struct {
		name string
		g    grants
		want map[string]int // probe -> expected status (200 or 403)
	}{
		{"no grants", grants{false, false, false, false}, map[string]int{
			"/api/datasources": 403, "/api/tables!POST": 403, "/api/audit": 403,
			"/api/hooks/outbox": 403, "/api/meta/export": 403, "/api/hooks": 403}},
		{"ds only", grants{true, false, false, false}, map[string]int{
			"/api/datasources": 200, "/api/tables!POST": 403, "/api/audit": 403,
			"/api/hooks/outbox": 403, "/api/meta/export": 403, "/api/hooks": 403}},
		{"tables only", grants{false, true, false, false}, map[string]int{
			"/api/datasources": 403, "/api/tables!POST": 200, "/api/audit": 403,
			"/api/hooks/outbox": 403, "/api/meta/export": 403, "/api/hooks": 200}},
		{"audit only", grants{false, false, true, false}, map[string]int{
			"/api/datasources": 403, "/api/tables!POST": 403, "/api/audit": 200,
			"/api/hooks/outbox": 403, "/api/meta/export": 403, "/api/hooks": 403}},
		{"outbox only", grants{false, false, false, true}, map[string]int{
			"/api/datasources": 403, "/api/tables!POST": 403, "/api/audit": 403,
			"/api/hooks/outbox": 200, "/api/meta/export": 403, "/api/hooks": 403}},
		{"ds+tables (transfer ok)", grants{true, true, false, false}, map[string]int{
			"/api/meta/export": 200}},
	}
	for _, tc := range cases {
		c := loginAs(t, s, "user_"+tc.name, roleWith(tc.name, tc.g), nil)
		for probe, want := range tc.want {
			if got := probeStatus(t, s, c, probe); got != want {
				t.Fatalf("%s: %s = %d, want %d", tc.name, probe, got, want)
			}
		}
	}
}

func TestMeReturnsSplitGrants(t *testing.T) {
	s := newTestServer(t)
	c := loginAs(t, s, "auditor", roleWith("Auditor", grants{false, false, true, false}), nil)
	w := do(s, "GET", "/api/auth/me", "", c)
	for _, want := range []string{`"manageDatasources":false`, `"manageTables":false`, `"viewAudit":true`, `"viewOutbox":false`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("me missing %s: %s", want, w.Body)
		}
	}
	// guard against the pre-v1.7 field sneaking back into /auth/me
	if strings.Contains(w.Body.String(), "platform"+"Manage") {
		t.Fatalf("me still returns the legacy platform field: %s", w.Body)
	}
}
