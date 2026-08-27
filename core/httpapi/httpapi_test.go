package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
	"github.com/luthfi9251/ku-crud/core/ds"
	"github.com/luthfi9251/ku-crud/core/httpapi"
)

// fakeSource stands in for the App registry; errAdapter's embedded nil
// interface panics on any real use, proving these tests never reach the
// datasource (they exercise routing/gate/guard paths only).
type fakeSource struct{ list []*defs.Table }

func (f *fakeSource) Resolve(name string) (*defs.Table, error) {
	for _, t := range f.list {
		if t.Name == name {
			return t, nil
		}
	}
	return nil, errors.New("not registered")
}
func (f *fakeSource) Adapter(*defs.Table) (ds.Adapter, error) { return errAdapter{}, nil }
func (f *fakeSource) Defs() []*defs.Table                     { return f.list }

type errAdapter struct{ ds.Adapter }

func testTable() *defs.Table {
	return &defs.Table{Name: "things", Label: "Things", PhysTab: "things",
		Schema: "public", Keys: []string{"id"}, PageSize: 20, SourceType: "table",
		Columns: []defs.Column{{Name: "id", Label: "Id", FieldType: "number",
			Editable: true, Required: true, Visible: true, Searchable: true, Sortable: true}}}
}

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body not JSON: %q", w.Body.String())
	}
	return m["code"].(string), m["message"].(string)
}

func TestResourceMountsAnywhere(t *testing.T) {
	src := &fakeSource{list: []*defs.Table{testTable()}}
	h := httpapi.New("things", src.list[0], src, httpapi.Options{})
	for _, prefix := range []string{"", "/api", "/api/v1/things", "/deep/nest/x"} {
		// wrong method proves the route resolved (405, not 404)
		w := serve(t, h, http.MethodPatch, prefix+"/rows")
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, POST" {
			t.Fatalf("prefix %q: %d %s", prefix, w.Code, w.Body.String())
		}
	}
}

func TestResourceUnknownRoute(t *testing.T) {
	src := &fakeSource{list: []*defs.Table{testTable()}}
	h := httpapi.New("things", src.list[0], src, httpapi.Options{})
	for _, p := range []string{"/x/bogus", "/x/rows/1/extra", "/x/import/nope", "/x/fkoptions"} {
		w := serve(t, h, http.MethodGet, p)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%q: %d", p, w.Code)
		}
		if code, _ := errCode(t, w); code != "NOT_FOUND" {
			t.Fatalf("%q: %s", p, w.Body.String())
		}
	}
}

func TestResourceGate(t *testing.T) {
	src := &fakeSource{list: []*defs.Table{testTable()}}
	var gotOp httpapi.Op
	var gotTable string
	gate := func(r *http.Request, op httpapi.Op, table string) error {
		gotOp, gotTable = op, table
		return errors.New("not you")
	}
	h := httpapi.New("things", src.list[0], src, httpapi.Options{Gate: gate})
	w := serve(t, h, http.MethodDelete, "/p/rows/AQ")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d", w.Code)
	}
	if code, msg := errCode(t, w); code != "FORBIDDEN" || msg != "not you" {
		t.Fatalf("body: %s", w.Body.String())
	}
	if gotOp != httpapi.OpDelete || gotTable != "things" {
		t.Fatalf("gate saw op=%q table=%q", gotOp, gotTable)
	}
	// every op class maps to its route
	for _, c := range []struct {
		method, path string
		op           httpapi.Op
	}{
		{http.MethodGet, "/p/rows", httpapi.OpRead},
		{http.MethodGet, "/p/rows/export", httpapi.OpExport},
		{http.MethodPost, "/p/rows", httpapi.OpCreate},
		{http.MethodPut, "/p/rows/AQ", httpapi.OpUpdate},
		{http.MethodPost, "/p/rows/bulk-delete", httpapi.OpDelete},
		{http.MethodGet, "/p/fkoptions/c", httpapi.OpRead},
		{http.MethodGet, "/p/m2moptions/c", httpapi.OpRead},
		{http.MethodGet, "/p/rows/AQ/m2m/c", httpapi.OpRead},
		{http.MethodPost, "/p/import/preview", httpapi.OpImport},
		{http.MethodPost, "/p/import/apply", httpapi.OpImport},
	} {
		w := serve(t, h, c.method, c.path)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s: %d %s", c.method, c.path, w.Code, w.Body.String())
		}
		if gotOp != c.op {
			t.Fatalf("%s %s: gate saw op=%q want %q", c.method, c.path, gotOp, c.op)
		}
	}
}

func TestResourceQueryReadOnly(t *testing.T) {
	q := testTable()
	q.SourceType, q.Schema, q.PhysTab, q.Keys = "query", "", "", nil
	src := &fakeSource{list: []*defs.Table{q}}
	h := httpapi.New("things", q, src, httpapi.Options{})
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/p/rows"},
		{http.MethodPut, "/p/rows/AQ"},
		{http.MethodDelete, "/p/rows/AQ"},
		{http.MethodPost, "/p/rows/bulk-delete"},
		{http.MethodPost, "/p/import/preview"},
		{http.MethodPost, "/p/import/apply"},
	} {
		w := serve(t, h, c.method, c.path)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s: %d", c.method, c.path, w.Code)
		}
		if code, _ := errCode(t, w); code != "QUERY_READONLY" {
			t.Fatalf("%s %s: %s", c.method, c.path, w.Body.String())
		}
	}
}

func TestDefsHandler(t *testing.T) {
	tbl := testTable()
	tbl.Hooks = `{"beforeCreate":[{"hook":"h","order":0}]}`
	junction := &defs.Table{Name: "jt", PhysTab: "jt", Keys: []string{"id"},
		Columns: []defs.Column{
			{Name: "id", FieldType: "number"},
			{Name: "thing_id", FieldType: "fk", FK: &defs.FK{Table: "things", RefColumn: "id"}},
			{Name: "tag_id", FieldType: "fk", FK: &defs.FK{Table: "tags", RefColumn: "id"}},
		}}
	tags := &defs.Table{Name: "tags", PhysTab: "tags", Keys: []string{"id"},
		Columns: []defs.Column{{Name: "id", FieldType: "number"}}}
	tbl.Columns = append(tbl.Columns, defs.Column{Name: "tags", FieldType: "m2m",
		M2M: &defs.M2M{JunctionTable: "jt", SrcCol: "thing_id", TgtCol: "tag_id"}})
	src := &fakeSource{list: []*defs.Table{tbl, junction, tags}}

	h := httpapi.DefsHandler(src, func(r *http.Request, op httpapi.Op, table string) error {
		return errors.New("no delete")
	})
	w := serve(t, h, http.MethodGet, "/api/defs")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("not a list: %s", w.Body.String())
	}
	if len(list) != 3 || list[0]["name"] != "things" {
		t.Fatalf("defs order/content: %s", w.Body.String())
	}
	d := list[0]
	if d["keyColumns"].([]any)[0] != "id" || d["permissions"].(map[string]any)["delete"] != false {
		t.Fatalf("dto: %s", w.Body.String())
	}
	if d["hooks"] == nil {
		t.Fatalf("hooks raw JSON missing: %s", w.Body.String())
	}
	var m2mcol map[string]any
	for _, c := range d["columns"].([]any) {
		if c.(map[string]any)["name"] == "tags" {
			m2mcol = c.(map[string]any)
		}
	}
	if m2mcol == nil || m2mcol["m2mRefColumn"] != "id" || m2mcol["m2mTargetRef"] != "id" {
		t.Fatalf("m2m ref columns unresolved: %s", w.Body.String())
	}
	// non-GET → 405
	if w := serve(t, h, http.MethodPost, "/api/defs"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post defs: %d", w.Code)
	}
}

func TestStatsAnchor(t *testing.T) {
	src := &fakeSource{list: []*defs.Table{testTable()}}
	h := httpapi.New("things", src.list[0], src, httpapi.Options{})

	// wrong method proves the route resolved (405, not 404)
	if w := serve(t, h, http.MethodPost, "/stats"); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET" {
		t.Fatalf("stats POST = %d %s", w.Code, w.Body)
	}
	// trailing segments are not part of the route
	if w := serve(t, h, http.MethodGet, "/stats/extra"); w.Code != http.StatusNotFound {
		t.Fatalf("stats/extra = %d", w.Code)
	}
	// gate denial fires with OpRead before any datasource use
	var gotOp httpapi.Op
	h2 := httpapi.New("things", src.list[0], src, httpapi.Options{
		Gate: func(r *http.Request, op httpapi.Op, table string) error {
			gotOp = op
			return errors.New("no")
		}})
	if w := serve(t, h2, http.MethodGet, "/stats?func=count"); w.Code != http.StatusForbidden {
		t.Fatalf("gated stats = %d %s", w.Code, w.Body)
	}
	if gotOp != httpapi.OpRead {
		t.Fatalf("op = %q", gotOp)
	}
}
