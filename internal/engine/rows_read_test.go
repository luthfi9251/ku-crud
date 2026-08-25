package engine

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/defs"
	"ku-crud/internal/ds"
)

// fakeAdapter implements just the read contract; any unexpected call
// panics through the nil embedded interface.
type fakeAdapter struct {
	ds.Adapter
	listRows    func(ds.ListParams) ([]map[string]any, error)
	countRows   func(ds.ListParams) (int, error)
	fetchByKey  func(schema, table string, keyCols []string, keyVals []any, cols []string) ([]map[string]any, error)
	fetchByRefs func(schema, table, refCol string, displayCols []string, vals []any) (map[string]map[string]any, error)
	fetchPairs  func(schema, table, col, retCol string, vals []any) ([]ds.Pair, error)
	listQuery   func(ds.QueryParams) ([]map[string]any, error)
	countQuery  func(ds.QueryParams) (int, error)
	insert      func(schema, table string, cols []string, vals []any) error
	updateByKey func(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (int64, error)
	deleteByKey func(schema, table string, keyCols []string, keyVals []any) (int64, error)
	countByRef  func(schema, table, col string, val any) (int, error)
	deletePairs func(schema, table, col1 string, val1 any, col2 string, val2 any) (int64, error)
	fkViolation func(err error) bool
	closes      int
}

func (f *fakeAdapter) ListRows(p ds.ListParams) ([]map[string]any, error) { return f.listRows(p) }
func (f *fakeAdapter) CountRows(p ds.ListParams) (int, error)             { return f.countRows(p) }
func (f *fakeAdapter) FetchByKey(sc, tb string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
	return f.fetchByKey(sc, tb, kc, kv, cols)
}
func (f *fakeAdapter) FetchByRefValues(sc, tb string, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
	return f.fetchByRefs(sc, tb, rc, dc, vals)
}
func (f *fakeAdapter) FetchPairsByRef(sc, tb string, col, retCol string, vals []any) ([]ds.Pair, error) {
	return f.fetchPairs(sc, tb, col, retCol, vals)
}
func (f *fakeAdapter) ListQueryRows(p ds.QueryParams) ([]map[string]any, error) {
	return f.listQuery(p)
}
func (f *fakeAdapter) CountQueryRows(p ds.QueryParams) (int, error) { return f.countQuery(p) }
func (f *fakeAdapter) Insert(sc, tb string, cols []string, vals []any) error {
	return f.insert(sc, tb, cols, vals)
}
func (f *fakeAdapter) UpdateByKey(sc, tb string, scols []string, svals []any, kcols []string, kvals []any) (int64, error) {
	return f.updateByKey(sc, tb, scols, svals, kcols, kvals)
}
func (f *fakeAdapter) DeleteByKey(sc, tb string, kcols []string, kvals []any) (int64, error) {
	return f.deleteByKey(sc, tb, kcols, kvals)
}
func (f *fakeAdapter) CountByRefEq(sc, tb, col string, val any) (int, error) {
	return f.countByRef(sc, tb, col, val)
}
func (f *fakeAdapter) DeletePairs(sc, tb string, col1 string, val1 any, col2 string, val2 any) (int64, error) {
	return f.deletePairs(sc, tb, col1, val1, col2, val2)
}
func (f *fakeAdapter) IsFKViolation(err error) bool {
	return f.fkViolation != nil && f.fkViolation(err)
}
func (f *fakeAdapter) Close() error { f.closes++; return nil }

// fakeResolver serves defs by name and dispatches adapters per table.
type fakeResolver struct {
	tables  map[string]*defs.Table
	adapter func(t *defs.Table) (ds.Adapter, error)
}

func (f *fakeResolver) Adapter(t *defs.Table) (ds.Adapter, error) { return f.adapter(t) }
func (f *fakeResolver) Resolve(name string) (*defs.Table, error) {
	t, ok := f.tables[name]
	if !ok {
		return nil, fmt.Errorf("definition %q not found", name)
	}
	return t, nil
}

func customersDef() *defs.Table {
	return &defs.Table{Name: "customers", Schema: "public", PhysTab: "customers",
		Keys: []string{"id"}, PageSize: 2, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
				Visible: true, Searchable: true, Sortable: true, Position: 0},
			{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
				Visible: true, Searchable: true, Sortable: true, Position: 1},
			{Name: "m2m_tags", Label: "Tags", FieldType: "m2m", Visible: true, Position: 2,
				M2M: &defs.M2M{JunctionTable: "customer_tags", SrcCol: "customer_id", TgtCol: "tag_id",
					DisplayColumns: []string{"label"}},
			},
		}}
}

func junctionDef() *defs.Table {
	return &defs.Table{Name: "customer_tags", Label: "Customer Tags", Schema: "public", PhysTab: "customer_tags",
		Keys: []string{"customer_id", "tag_id"}, Columns: []defs.Column{
			{Name: "customer_id", Label: "Customer", FieldType: "fk",
				Required: true, Visible: true, Position: 0,
				FK: &defs.FK{Table: "customers", RefColumn: "id", DisplayColumns: []string{"name"}}},
			{Name: "tag_id", Label: "Tag", FieldType: "fk",
				Required: true, Visible: true, Position: 1,
				FK: &defs.FK{Table: "tags", RefColumn: "id", DisplayColumns: []string{"label"}}},
		}}
}

func tagsDef() *defs.Table {
	return &defs.Table{Name: "tags", Schema: "public", PhysTab: "tags",
		Keys: []string{"id"}, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Position: 0},
			{Name: "label", Label: "Label", FieldType: "text", Visible: true, Position: 1},
		}}
}

func doRead(svc *ReadService, method, target, pk string) *httptest.ResponseRecorder {
	return doReadT(svc, method, target, pk, svc.R.(*fakeResolver).tables["customers"])
}

func doReadT(svc *ReadService, method, target, pk string, t *defs.Table) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.SetPathValue("pk", pk)
	w := httptest.NewRecorder()
	switch method {
	case "GET":
		if strings.Contains(target, "export") {
			svc.ExportCSV(w, r, t)
		} else if pk != "" {
			svc.Get(w, r, t)
		} else {
			svc.List(w, r, t)
		}
	default:
		panic("method " + method)
	}
	return w
}

func TestReadList(t *testing.T) {
	pages := map[int][]map[string]any{
		1: {{"id": 1.0, "name": "jo"}, {"id": 2.0, "name": "joe"}},
		2: {{"id": 3.0, "name": "ana"}},
	}
	res := &fakeResolver{tables: map[string]*defs.Table{
		"customers": customersDef(), "customer_tags": junctionDef(), "tags": tagsDef(),
	}}
	seen := map[string][]any{}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers":
			return &fakeAdapter{
				listRows: func(p ds.ListParams) ([]map[string]any, error) {
					seen["search"] = []any{p.Search, p.SortCol, p.SortDir}
					return pages[(p.Offset/p.Limit)+1], nil
				},
				countRows: func(p ds.ListParams) (int, error) { return 3, nil },
			}, nil
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb, col, retCol string, vals []any) ([]ds.Pair, error) {
					return []ds.Pair{{Col: 1.0, Ret: 10.0}, {Col: 1.0, Ret: 11.0}, {Col: 3.0, Ret: 12.0}}, nil
				},
			}, nil
		case "tags":
			return &fakeAdapter{
				fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
					return map[string]map[string]any{
						"10": {"id": 10.0, "label": "vip"},
						"11": {"id": 11.0, "label": "beta"},
						"12": {"id": 12.0, "label": "lead"},
					}, nil
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}
	svc := &ReadService{R: res}

	w := doRead(svc, "GET", "/?search=jo&sort=name&dir=DESC", "")
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	// rel-less tables serialize rels/m2mRels as null (pinned behavior)
	for _, want := range []string{`"total":3`, `"page":1`, `"pageSize":2`, `"rows":[`,
		`"m2mRels":{"m2m_tags":{"1":[{"id":10,"label":"vip"},{"id":11,"label":"beta"}]`,
		`"rels":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if got := seen["search"]; got[0] != "jo" || got[1] != "name" || got[2] != "DESC" {
		t.Fatalf("params not passed through: %v", got)
	}

	// page 2 requested
	w = doRead(svc, "GET", "/?page=2", "")
	if !strings.Contains(w.Body.String(), `"page":2`) || !strings.Contains(w.Body.String(), `"name":"ana"`) {
		t.Fatalf("page2 = %s", w.Body)
	}

	// invalid filter → 400 FILTER_INVALID
	w = doRead(svc, "GET", "/?filters=notjson", "")
	if w.Code != 400 || !strings.Contains(w.Body.String(), "FILTER_INVALID") {
		t.Fatalf("bad filter = %d %s", w.Code, w.Body)
	}
}

func TestReadListEmptyRowsAndComputed(t *testing.T) {
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": {
		Name: "customers", Schema: "public", PhysTab: "customers", Keys: []string{"id"}, PageSize: 10,
		Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Sortable: true},
			{Name: "name", Label: "Name", FieldType: "text", Visible: true, Sortable: true},
			{Name: "dup", Label: "Dup", FieldType: "text", Visible: true, IsComputed: true,
				ComputedFormula: `CONCAT(name, "!")`},
		},
	}}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			listRows: func(p ds.ListParams) ([]map[string]any, error) {
				return []map[string]any{{"id": 1.0, "name": "jo"}}, nil
			},
			countRows: func(p ds.ListParams) (int, error) { return 1, nil },
		}, nil
	}
	svc := &ReadService{R: res}
	w := doRead(svc, "GET", "/", "")
	if !strings.Contains(w.Body.String(), `"dup":"jo!"`) {
		t.Fatalf("computed column missing: %s", w.Body)
	}
	// empty result serializes rows as [] (not null)
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			listRows:  func(p ds.ListParams) ([]map[string]any, error) { return nil, nil },
			countRows: func(p ds.ListParams) (int, error) { return 0, nil },
		}, nil
	}
	w = doRead(svc, "GET", "/", "")
	if !strings.Contains(w.Body.String(), `"rows":[]`) || !strings.Contains(w.Body.String(), `"rels":null`) {
		t.Fatalf("empty shape = %s", w.Body)
	}
}

func TestReadListFKRelsAndGrants(t *testing.T) {
	orders := &defs.Table{Name: "orders", Schema: "public", PhysTab: "orders",
		Keys: []string{"id"}, PageSize: 10, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Sortable: true},
			{Name: "note", Label: "Note", FieldType: "text", Visible: true, Sortable: true},
			{Name: "customer_id", Label: "Customer", FieldType: "fk", Visible: true, Sortable: true,
				FK: &defs.FK{Table: "customers", RefColumn: "id", DisplayColumns: []string{"name"}}},
			{Name: "parent_id", Label: "Parent", FieldType: "fk", Visible: true, Sortable: true,
				FK: &defs.FK{Table: "", RefColumn: "id", DisplayColumns: []string{"note"}}}, // self-fk
		}}
	res := &fakeResolver{tables: map[string]*defs.Table{"orders": orders, "customers": customersDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "orders":
			return &fakeAdapter{
				listRows: func(p ds.ListParams) ([]map[string]any, error) {
					return []map[string]any{{"id": 1.0, "note": "o1", "customer_id": 7.0, "parent_id": 2.0}}, nil
				},
				countRows: func(p ds.ListParams) (int, error) { return 1, nil },
				fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
					// self-fk lookups echo the requested ref values
					out := map[string]map[string]any{}
					for _, v := range vals {
						key := rowValKey(v)
						out[key] = map[string]any{"id": v, "note": "o" + key}
					}
					return out, nil
				},
			}, nil
		case "customers":
			return &fakeAdapter{
				fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
					return map[string]map[string]any{"7": {"id": 7.0, "name": "jo"}}, nil
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}

	// with grants: fk rel resolves via name, self-fk resolves against t
	svc := &ReadService{R: res, CanRead: func(string) bool { return true }}
	w := doReadT(svc, "GET", "/", "", orders)
	body := w.Body.String()
	if !strings.Contains(body, `"rels":{"customer_id"`) || !strings.Contains(body, `"name":"jo"`) {
		t.Fatalf("fk rels missing: %s", body)
	}
	if !strings.Contains(body, `"parent_id":{"2":{"id":2,"note":"o2"}}`) {
		t.Fatalf("self-fk rels missing: %s", body)
	}

	// without the target grant the fk column is skipped; the self-fk still
	// resolves (the main table's read grant was checked by the caller)
	svc = &ReadService{R: res, CanRead: func(name string) bool { return name != "customers" }}
	w = doReadT(svc, "GET", "/", "", orders)
	if w.Code != 200 {
		t.Fatalf("ungranted fk list = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"rels":{"parent_id"`) {
		t.Fatalf("self-fk must survive target-grant skip: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), `"customer_id":{"7"`) {
		t.Fatalf("ungranted fk leaked: %s", w.Body)
	}
}

func TestReadGet(t *testing.T) {
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": customersDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			fetchByKey: func(sc, tb string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
				if fmt.Sprint(kv[0]) == "2" {
					return []map[string]any{{"id": 2.0, "name": "joe"}}, nil
				}
				return nil, nil
			},
		}, nil
	}
	svc := &ReadService{R: res}

	w := doRead(svc, "GET", "/", EncodeRowKey([]string{"2"}))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"joe"`) ||
		!strings.Contains(w.Body.String(), `"rels":null`) {
		t.Fatalf("get = %d %s", w.Code, w.Body)
	}
	if w = doRead(svc, "GET", "/", EncodeRowKey([]string{"999"})); w.Code != 404 {
		t.Fatalf("missing = %d", w.Code)
	}
	if w = doRead(svc, "GET", "/", "notakey"); w.Code != 400 {
		t.Fatalf("bad key = %d", w.Code)
	}
	// key count mismatch → 400
	if w = doRead(svc, "GET", "/", EncodeRowKey([]string{"1", "2"})); w.Code != 400 {
		t.Fatalf("composite key mismatch = %d", w.Code)
	}
}

func TestReadQueryViews(t *testing.T) {
	qv := &defs.Table{Name: "qv", Schema: "public", PhysTab: "qv", SourceType: "query",
		QuerySQL: "SELECT 1 AS x", Keys: []string{"x"}, PageSize: 5, Columns: []defs.Column{
			{Name: "x", Label: "X", FieldType: "number", Visible: true, Sortable: true},
		}}
	res := &fakeResolver{tables: map[string]*defs.Table{"qv": qv}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			listQuery:  func(p ds.QueryParams) ([]map[string]any, error) { return []map[string]any{{"x": 4.0}}, nil },
			countQuery: func(p ds.QueryParams) (int, error) { return 1, nil },
		}, nil
	}
	svc := &ReadService{R: res}
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	svc.List(w, r, qv)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"x":4`) {
		t.Fatalf("query list = %d %s", w.Code, w.Body)
	}

	// query view with nothing sortable (and no key to fall back to) → 400
	nosort := *qv
	nosort.Keys = nil
	nosort.Columns = []defs.Column{{Name: "x", Label: "X", FieldType: "number", Visible: true}}
	w = httptest.NewRecorder()
	svc.List(w, httptest.NewRequest("GET", "/", nil), &nosort)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "query view has no sortable column") {
		t.Fatalf("no sortable = %d %s", w.Code, w.Body)
	}

	// query view without key columns → get rejected before the adapter
	nokey := *qv
	nokey.Keys = nil
	nokey.Columns = []defs.Column{{Name: "x", Label: "X", FieldType: "number", Visible: true}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		t.Fatal("adapter must not open for QUERY_NO_KEY")
		return nil, nil
	}
	w = httptest.NewRecorder()
	svc.Get(w, httptest.NewRequest("GET", "/", nil), &nokey)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "QUERY_NO_KEY") {
		t.Fatalf("no key = %d %s", w.Code, w.Body)
	}
}

func TestReadAdapterErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		body string
	}{
		{ErrDSNotFound, 404, `"code":"NOT_FOUND"`},
		{ErrConn, 502, `"code":"CONN"`},
		{fmt.Errorf("boom"), 500, `"code":"INTERNAL"`},
	} {
		res := &fakeResolver{tables: map[string]*defs.Table{"customers": customersDef()}}
		res.adapter = func(tb *defs.Table) (ds.Adapter, error) { return nil, tc.err }
		svc := &ReadService{R: res}
		w := doRead(svc, "GET", "/", "")
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("err %v = %d %s", tc.err, w.Code, w.Body)
		}
	}
}

func TestReadM2MRelsJunctionGrants(t *testing.T) {
	res := &fakeResolver{tables: map[string]*defs.Table{
		"customers": customersDef(), "customer_tags": junctionDef(), "tags": tagsDef(),
	}}
	var junctionOpens int
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers":
			return &fakeAdapter{
				listRows: func(p ds.ListParams) ([]map[string]any, error) {
					return []map[string]any{{"id": 1.0, "name": "jo"}}, nil
				},
				countRows: func(p ds.ListParams) (int, error) { return 1, nil },
			}, nil
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb, col, retCol string, vals []any) ([]ds.Pair, error) {
					junctionOpens++
					return []ds.Pair{{Col: 1.0, Ret: 10.0}}, nil
				},
			}, nil
		case "tags":
			return &fakeAdapter{
				fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
					return map[string]map[string]any{"10": {"id": 10.0, "label": "vip"}}, nil
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}

	// both grants → m2m rels rendered
	svc := &ReadService{R: res, CanRead: func(string) bool { return true }}
	w := doRead(svc, "GET", "/", "")
	if !strings.Contains(w.Body.String(), `"m2mRels":{"m2m_tags":{"1":[{"id":10,"label":"vip"}]}}`) {
		t.Fatalf("m2m rels = %s", w.Body)
	}
	if junctionOpens != 1 {
		t.Fatalf("junction opened %d times", junctionOpens)
	}

	// no read on the junction → skipped before any junction query
	svc = &ReadService{R: res, CanRead: func(name string) bool { return name != "customer_tags" }}
	w = doRead(svc, "GET", "/", "")
	if strings.Contains(w.Body.String(), `"m2m_tags"`) {
		t.Fatalf("m2m rels leaked without junction grant: %s", w.Body)
	}

	// drifted junction (required column outside the two link columns) renders nothing
	drifted := junctionDef()
	drifted.Columns = append(drifted.Columns, defs.Column{Name: "note", FieldType: "text", Required: true})
	res.tables["customer_tags"] = drifted
	svc = &ReadService{R: res, CanRead: func(string) bool { return true }}
	w = doRead(svc, "GET", "/", "")
	if strings.Contains(w.Body.String(), `"m2m_tags"`) || w.Code != 200 {
		t.Fatalf("drifted junction must render nothing: %d %s", w.Code, w.Body)
	}
}

func TestExportCSVMoved(t *testing.T) {
	orders := &defs.Table{Name: "orders", Schema: "public", PhysTab: "orders",
		Keys: []string{"id"}, PageSize: 10, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Sortable: true},
			{Name: "note", Label: "Note", FieldType: "text", Visible: true, Sortable: true},
			{Name: "customer_id", Label: "Customer", FieldType: "fk", Visible: true, Sortable: true,
				FK: &defs.FK{Table: "customers", RefColumn: "id", DisplayColumns: []string{"name", "city"}},
			},
			{Name: "hidden", Label: "Hidden", FieldType: "text", Visible: false},
		}}
	res := &fakeResolver{tables: map[string]*defs.Table{"orders": orders, "customers": customersDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "orders":
			return &fakeAdapter{
				listRows: func(p ds.ListParams) ([]map[string]any, error) {
					if p.Limit != exportRowCap+1 {
						t.Errorf("export limit = %d, want %d", p.Limit, exportRowCap+1)
					}
					return []map[string]any{{"id": 1.0, "note": "a,b", "customer_id": 7.0, "hidden": "x"}}, nil
				},
				countRows: func(p ds.ListParams) (int, error) { return 1, nil },
			}, nil
		case "customers":
			return &fakeAdapter{
				fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
					return map[string]map[string]any{"7": {"id": 7.0, "name": "jo", "city": "Bandung"}}, nil
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}
	svc := &ReadService{R: res, CanRead: func(string) bool { return true }}
	w := doReadT(svc, "GET", "/rows/export", "", orders)
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatal("BOM missing")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), `filename="orders-`) {
		t.Fatalf("disposition = %q", w.Header().Get("Content-Disposition"))
	}
	body = body[3:]
	if !strings.HasPrefix(body, "id,note,customer_id\n") {
		t.Fatalf("header = %q", body)
	}
	if !strings.Contains(body, `1,"a,b",jo — Bandung`) {
		t.Fatalf("row = %q", body)
	}
	if strings.Contains(body, "hidden") {
		t.Fatalf("invisible column leaked: %q", body)
	}

	// over the cap → EXPORT_TOO_LARGE
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		if tb.Name != "orders" {
			return nil, fmt.Errorf("unexpected table %s", tb.Name)
		}
		return &fakeAdapter{
			countRows: func(p ds.ListParams) (int, error) { return exportRowCap + 1, nil },
		}, nil
	}
	w = doReadT(svc, "GET", "/rows/export", "", orders)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "EXPORT_TOO_LARGE") {
		t.Fatalf("cap = %d %s", w.Code, w.Body)
	}
}

// moved from internal/api/export_test.go with csvCell/joinDisplay
func TestCSVCell(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{true, "true"},
		{false, "false"},
		{"plain", "plain"},
		{[]byte("bytes"), "bytes"},
		{float64(10.5), "10.5"},
		{float64(2), "2"},
		{int64(7), "7"},
	}
	for _, c := range cases {
		if got := csvCell(c.in); got != c.want {
			t.Errorf("csvCell(%v) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestJoinDisplay(t *testing.T) {
	rel := map[string]any{"id": 3.0, "name": "jo", "email": "jo@x.io"}
	if got := joinDisplay(rel, []string{"name", "email"}, "id"); got != "jo — jo@x.io" {
		t.Fatalf("joinDisplay = %q", got)
	}
	// no display columns configured → ref column value
	if got := joinDisplay(rel, nil, "id"); got != "3" {
		t.Fatalf("fallback = %q", got)
	}
}
