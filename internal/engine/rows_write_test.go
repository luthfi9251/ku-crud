package engine

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/defs"
	"ku-crud/internal/ds"
)

// fakeHooks records the callback flow; before may mutate the payload.
type fakeHooks struct {
	guardErr  error
	before    func(ev Event, t *defs.Table, row RowPayload) (RowPayload, error)
	beforeLog []string
	afterLog  []string
	afterSnap []RowPayload
}

func (f *fakeHooks) Guard(t *defs.Table) error { return f.guardErr }

func (f *fakeHooks) RunBefore(ev Event, t *defs.Table, row RowPayload) (RowPayload, error) {
	f.beforeLog = append(f.beforeLog, string(ev)+":"+t.Name)
	if f.before != nil {
		return f.before(ev, t, row)
	}
	return row, nil
}

func (f *fakeHooks) RunAfter(ev Event, t *defs.Table, row RowPayload) error {
	f.afterLog = append(f.afterLog, string(ev)+":"+t.Name)
	f.afterSnap = append(f.afterSnap, row)
	return nil
}

// customersWriteDef is the write fixture: number key, editable name, an
// fk to regions and an m2m virtual column over customer_tags.
func customersWriteDef() *defs.Table {
	return &defs.Table{Name: "customers", Schema: "public", PhysTab: "customers",
		Keys: []string{"id"}, PageSize: 2, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
				Visible: true, Position: 0},
			{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
				Visible: true, Position: 1},
			{Name: "region_id", Label: "Region", FieldType: "fk", BaseType: "number",
				Editable: true, Visible: true, Position: 2,
				FK: &defs.FK{Table: "regions", RefColumn: "id", DisplayColumns: []string{"label"}},
			},
			{Name: "m2m_tags", Label: "Tags", FieldType: "m2m", Visible: true, Position: 3,
				M2M: &defs.M2M{JunctionTable: "customer_tags", SrcCol: "customer_id", TgtCol: "tag_id",
					DisplayColumns: []string{"label"}},
			},
		}}
}

func regionsDef() *defs.Table {
	return &defs.Table{Name: "regions", Schema: "public", PhysTab: "regions",
		Keys: []string{"id"}, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Position: 0},
			{Name: "label", Label: "Label", FieldType: "text", Visible: true, Position: 1},
		}}
}

func ordersRefDef() *defs.Table {
	return &defs.Table{Name: "orders", Schema: "public", PhysTab: "orders", Keys: []string{"id"},
		Columns: []defs.Column{{Name: "id"}, {Name: "customer_id"}}}
}

func writeResolver() *fakeResolver {
	return &fakeResolver{tables: map[string]*defs.Table{
		"customers": customersWriteDef(), "customer_tags": junctionDef(),
		"tags": tagsDef(), "regions": regionsDef(), "orders": ordersRefDef(),
	}}
}

func doWrite(svc *WriteService, method, pk, body string, t *defs.Table) *httptest.ResponseRecorder {
	var rdr strings.Reader
	if body != "" {
		rdr = *strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "/rows", &rdr)
	r.SetPathValue("pk", pk)
	w := httptest.NewRecorder()
	switch method {
	case "POST":
		svc.Insert(w, r, t)
	case "PUT":
		svc.Update(w, r, t)
	case "DELETE":
		svc.Delete(w, r, t)
	}
	return w
}

func doBulk(svc *WriteService, body string, t *defs.Table) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/rows/bulk-delete", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.BulkDelete(w, r, t)
	return w
}

func TestWriteInsert(t *testing.T) {
	res := writeResolver()
	h := &fakeHooks{}
	var gotCols []string
	var gotVals []any
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error {
			gotCols, gotVals = cols, vals
			return nil
		}}, nil
	}
	svc := &WriteService{R: res, H: h}
	w := doWrite(svc, "POST", "", `{"id":7,"name":"nia"}`, res.tables["customers"])
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("insert = %d %s", w.Code, w.Body)
	}
	if len(gotCols) != 2 || gotCols[0] != "id" || gotCols[1] != "name" ||
		gotVals[0] != float64(7) || gotVals[1] != "nia" {
		t.Fatalf("insert args = %v %v", gotCols, gotVals)
	}
	if len(h.afterLog) != 1 || h.afterLog[0] != "afterCreate:customers" {
		t.Fatalf("after log = %v", h.afterLog)
	}
	if h.afterSnap[0].Values["name"] != "nia" || h.afterSnap[0].Old != nil {
		t.Fatalf("after snapshot = %+v", h.afterSnap[0])
	}
}

func TestWriteInsertValidation(t *testing.T) {
	res := writeResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) { return &fakeAdapter{}, nil }
	svc := &WriteService{R: res}
	c := res.tables["customers"]
	if w := doWrite(svc, "POST", "", `{"name":"x","hax":1}`, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "VALIDATION") {
		t.Fatalf("unknown col = %d %s", w.Code, w.Body)
	}
	if w := doWrite(svc, "POST", "", `{"id":9}`, c); w.Code != 400 {
		t.Fatalf("missing required = %d %s", w.Code, w.Body)
	}
	if w := doWrite(svc, "POST", "", `not json`, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "invalid JSON body") {
		t.Fatalf("bad json = %d %s", w.Code, w.Body)
	}
}

func TestWriteInsertFKRefNotFound(t *testing.T) {
	res := writeResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		if tb.Name == "regions" {
			return &fakeAdapter{fetchByRefs: func(sc, tb2, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
				return map[string]map[string]any{}, nil
			}}, nil
		}
		return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error {
			t.Fatal("insert must not run when the fk ref is missing")
			return nil
		}}, nil
	}
	svc := &WriteService{R: res}
	w := doWrite(svc, "POST", "", `{"name":"x","region_id":99}`, res.tables["customers"])
	if w.Code != 400 || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("fk miss = %d %s", w.Code, w.Body)
	}
}

func TestWriteInsertHookMissingAndReject(t *testing.T) {
	res := writeResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) { return &fakeAdapter{}, nil }
	c := res.tables["customers"]
	svc := &WriteService{R: res, H: &fakeHooks{
		guardErr: &HookError{Missing: true, Msg: "hook Ghost is not registered in this binary"},
	}}
	if w := doWrite(svc, "POST", "", `{"name":"x"}`, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "HOOK_MISSING") {
		t.Fatalf("guard missing = %d %s", w.Code, w.Body)
	}
	svc = &WriteService{R: res, H: &fakeHooks{
		before: func(ev Event, tb *defs.Table, row RowPayload) (RowPayload, error) {
			return row, errors.New("nope")
		},
	}}
	if w := doWrite(svc, "POST", "", `{"name":"x"}`, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "HOOK_REJECTED") {
		t.Fatalf("before reject = %d %s", w.Code, w.Body)
	}
}

func TestWriteUpdateMutateAndAfterSnapshot(t *testing.T) {
	res := writeResolver()
	h := &fakeHooks{before: func(ev Event, tb *defs.Table, row RowPayload) (RowPayload, error) {
		row.Values["name"] = strings.ToUpper(row.Values["name"].(string))
		return row, nil
	}}
	var setCols []string
	var setVals []any
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			fetchByKey: func(sc, tb2 string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
				return []map[string]any{{"id": float64(1), "name": "jo"}}, nil
			},
			updateByKey: func(sc, tb2 string, scols []string, svals []any, kcols []string, kvals []any) (int64, error) {
				setCols, setVals = scols, svals
				return 1, nil
			},
		}, nil
	}
	svc := &WriteService{R: res, H: h}
	w := doWrite(svc, "PUT", EncodeRowKey([]string{"1"}), `{"name":"mia"}`, res.tables["customers"])
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"affected":1`) {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	if len(setCols) != 1 || setCols[0] != "name" || setVals[0] != "MIA" {
		t.Fatalf("update args = %v %v (before-hook mutation lost)", setCols, setVals)
	}
	if len(h.afterLog) != 1 || h.afterLog[0] != "afterUpdate:customers" {
		t.Fatalf("after log = %v", h.afterLog)
	}
	snap := h.afterSnap[0]
	if snap.Old["name"] != "jo" || snap.Values["name"] != "MIA" || snap.Values["id"] != float64(1) {
		t.Fatalf("after snapshot old=%v new=%v (must be merged)", snap.Old, snap.Values)
	}
}

func TestWriteUpdateNotFoundAndBadKey(t *testing.T) {
	res := writeResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{fetchByKey: func(sc, tb2 string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
			return nil, nil
		}}, nil
	}
	svc := &WriteService{R: res}
	c := res.tables["customers"]
	if w := doWrite(svc, "PUT", EncodeRowKey([]string{"42"}), `{"name":"x"}`, c); w.Code != 404 {
		t.Fatalf("not found = %d %s", w.Code, w.Body)
	}
	if w := doWrite(svc, "PUT", "garbage", `{"name":"x"}`, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "bad row key") {
		t.Fatalf("bad key = %d %s", w.Code, w.Body)
	}
}

func TestWriteDeleteConflictAndHappy(t *testing.T) {
	res := writeResolver()
	var deleted []any
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		if tb.Name == "orders" {
			return &fakeAdapter{countByRef: func(sc, tb2, col string, val any) (int, error) {
				return 2, nil
			}}, nil
		}
		return &fakeAdapter{
			fetchByKey: func(sc, tb2 string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
				return []map[string]any{{"id": float64(1), "name": "jo"}}, nil
			},
			deleteByKey: func(sc, tb2 string, kc []string, kv []any) (int64, error) {
				deleted = kv
				return 1, nil
			},
		}, nil
	}
	h := &fakeHooks{}
	svc := &WriteService{R: res, H: h,
		RefSources: func(t *defs.Table) ([]RefSource, error) {
			return []RefSource{{Src: res.tables["orders"], Column: "customer_id",
				RefColumn: "id", Label: "Orders"}}, nil
		}}
	w := doWrite(svc, "DELETE", EncodeRowKey([]string{"1"}), "", res.tables["customers"])
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Orders") ||
		!strings.Contains(w.Body.String(), `"count":2`) {
		t.Fatalf("conflict = %d %s", w.Code, w.Body)
	}
	if deleted != nil {
		t.Fatal("conflicted row must not be deleted")
	}
	svc.RefSources = func(t *defs.Table) ([]RefSource, error) { return nil, nil }
	w = doWrite(svc, "DELETE", EncodeRowKey([]string{"1"}), "", res.tables["customers"])
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"affected":1`) {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if len(h.beforeLog) != 1 || h.beforeLog[0] != "beforeDelete:customers" {
		t.Fatalf("before log = %v", h.beforeLog)
	}
	if len(h.afterLog) != 1 || h.afterLog[0] != "afterDelete:customers" ||
		h.afterSnap[0].Old["name"] != "jo" {
		t.Fatalf("after log = %v snap = %+v", h.afterLog, h.afterSnap)
	}
}

func TestWriteM2MSync(t *testing.T) {
	res := writeResolver()
	h := &fakeHooks{}
	var jInserts [][2]any
	var jDeletes [][2]any
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb2, col, retCol string, vals []any) ([]ds.Pair, error) {
					if fmt.Sprint(vals[0]) != "1" {
						return nil, nil
					}
					return []ds.Pair{{Col: int64(1), Ret: float64(1)}, {Col: int64(1), Ret: float64(2)}}, nil
				},
				insert: func(sc, tb2 string, cols []string, vals []any) error {
					jInserts = append(jInserts, [2]any{vals[0], vals[1]})
					return nil
				},
				deletePairs: func(sc, tb2 string, col1 string, val1 any, col2 string, val2 any) (int64, error) {
					jDeletes = append(jDeletes, [2]any{val1, val2})
					return 1, nil
				},
			}, nil
		default:
			return &fakeAdapter{
				fetchByKey: func(sc, tb2 string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
					return []map[string]any{{"id": float64(1), "name": "jo"}}, nil
				},
				updateByKey: func(sc, tb2 string, scols []string, svals []any, kcols []string, kvals []any) (int64, error) {
					return 1, nil
				},
			}, nil
		}
	}
	svc := &WriteService{R: res, H: h}
	w := doWrite(svc, "PUT", EncodeRowKey([]string{"1"}), `{"name":"jo","m2m_tags":[1,3]}`, res.tables["customers"])
	if w.Code != 200 {
		t.Fatalf("m2m update = %d %s", w.Code, w.Body)
	}
	if len(jInserts) != 1 || jInserts[0][1] != float64(3) {
		t.Fatalf("junction inserts = %v (want add of tag 3)", jInserts)
	}
	if len(jDeletes) != 1 || jDeletes[0][1] != float64(2) {
		t.Fatalf("junction deletes = %v (want removal of tag 2)", jDeletes)
	}
	seen := map[string]bool{}
	for _, e := range append(append([]string{}, h.beforeLog...), h.afterLog...) {
		seen[e] = true
	}
	for _, want := range []string{
		"beforeCreate:customer_tags", "afterCreate:customer_tags",
		"beforeDelete:customer_tags", "afterDelete:customer_tags",
	} {
		if !seen[want] {
			t.Fatalf("junction hook call %q missing; before=%v after=%v", want, h.beforeLog, h.afterLog)
		}
	}
}

func TestWriteM2MJunctionGrantDenied(t *testing.T) {
	res := writeResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) { return &fakeAdapter{}, nil }
	svc := &WriteService{R: res, CanWrite: func(table, grant string) bool { return false }}
	w := doWrite(svc, "PUT", EncodeRowKey([]string{"1"}), `{"name":"jo","m2m_tags":[1]}`, res.tables["customers"])
	if w.Code != 403 || !strings.Contains(w.Body.String(), "Customer Tags") {
		t.Fatalf("junction grant = %d %s", w.Code, w.Body)
	}
}

func TestWriteBulkDelete(t *testing.T) {
	res := writeResolver()
	h := &fakeHooks{}
	rows := map[string]bool{"1": true, "2": true}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			fetchByKey: func(sc, tb2 string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
				if rows[fmt.Sprint(kv[0])] {
					return []map[string]any{{"id": kv[0], "name": "row" + fmt.Sprint(kv[0])}}, nil
				}
				return nil, nil
			},
			deleteByKey: func(sc, tb2 string, kc []string, kv []any) (int64, error) {
				delete(rows, fmt.Sprint(kv[0]))
				return 1, nil
			},
		}, nil
	}
	svc := &WriteService{R: res, H: h,
		RefSources: func(t *defs.Table) ([]RefSource, error) { return nil, nil }}
	c := res.tables["customers"]
	if w := doBulk(svc, `{"keys":[]}`, c); w.Code != 400 {
		t.Fatalf("empty keys = %d %s", w.Code, w.Body)
	}
	var many []string
	for i := 0; i < 1001; i++ {
		many = append(many, EncodeRowKey([]string{"1"}))
	}
	if w := doBulk(svc, `{"keys":["`+strings.Join(many, `","`)+`"]}`, c); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "BULK_TOO_LARGE") {
		t.Fatalf("cap = %d %s", w.Code, w.Body)
	}
	// keys: 1 (deleted), 1 (deduped), 999 (not found) → partial success
	keys := `["` + EncodeRowKey([]string{"1"}) + `","` + EncodeRowKey([]string{"1"}) + `","` + EncodeRowKey([]string{"999"}) + `"]`
	w := doBulk(svc, `{"keys":`+keys+`}`, c)
	if w.Code != 200 {
		t.Fatalf("bulk = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"deleted":1`) || !strings.Contains(body, `"failed":1`) ||
		!strings.Contains(body, "NOT_FOUND") {
		t.Fatalf("bulk body = %s", body)
	}
	if len(h.afterLog) != 1 || h.afterLog[0] != "afterDelete:customers" {
		t.Fatalf("after log = %v (one entry per deleted row, after the loop)", h.afterLog)
	}
}
