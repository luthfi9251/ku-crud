package engine

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-crud/core/defs"
	"github.com/luthfi9251/ku-crud/core/ds"
)

func doRels(svc *ReadService, endpoint string, t *defs.Table, column, pk, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/"+endpoint+query, nil)
	r.SetPathValue("column", column)
	r.SetPathValue("pk", pk)
	w := httptest.NewRecorder()
	switch endpoint {
	case "fkoptions":
		svc.FKOptions(w, r, t)
	case "m2moptions":
		svc.M2MOptions(w, r, t)
	case "m2m":
		svc.M2MLinks(w, r, t)
	}
	return w
}

// relsResolver wires customers (m2m_tags → tags over customer_tags) and an
// orders table with an fk to customers plus a self-referencing parent_id.
func relsResolver() *fakeResolver {
	orders := &defs.Table{Name: "orders", Schema: "public", PhysTab: "orders",
		Keys: []string{"id"}, PageSize: 10, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Sortable: true},
			{Name: "note", Label: "Note", FieldType: "text", Searchable: true, Visible: true, Sortable: true},
			{Name: "customer_id", Label: "Customer", FieldType: "fk",
				FK: &defs.FK{Table: "customers", RefColumn: "id", DisplayColumns: []string{"name"}}},
			{Name: "parent_id", Label: "Parent", FieldType: "fk",
				FK: &defs.FK{Table: "", RefColumn: "id", DisplayColumns: []string{"note"}}},
		}}
	return &fakeResolver{tables: map[string]*defs.Table{
		"customers": customersDef(), "customer_tags": junctionDef(), "tags": tagsDef(),
		"orders": orders,
	}}
}

func TestResolveM2MMessages(t *testing.T) {
	res := relsResolver()
	base := customersDef()
	cases := []struct {
		name string
		m2m  *defs.M2M
		want string
	}{
		{"no m2m", nil, "junction definition not found (save it first)"},
		{"junction missing", &defs.M2M{JunctionTable: "nope", SrcCol: "customer_id", TgtCol: "tag_id"},
			"junction definition not found (save it first)"},
		{"junction self", &defs.M2M{JunctionTable: "customers", SrcCol: "customer_id", TgtCol: "tag_id"},
			"junction cannot be this table itself"},
		{"src not fk", &defs.M2M{JunctionTable: "customer_tags", SrcCol: "name", TgtCol: "tag_id"},
			"junction source/target columns must be defined fk columns"},
		{"src equals tgt", &defs.M2M{JunctionTable: "customer_tags", SrcCol: "tag_id", TgtCol: "tag_id"},
			"junction source and target columns must differ"},
		{"src not this table", &defs.M2M{JunctionTable: "customer_tags", SrcCol: "tag_id", TgtCol: "customer_id"},
			"junction source column must reference this table"},
	}
	for _, tc := range cases {
		col := defs.Column{Name: "m2m_tags", FieldType: "m2m", M2M: tc.m2m}
		cfg, msg := ResolveM2M(res, base, col)
		if cfg != nil || !strings.Contains(msg, tc.want) {
			t.Fatalf("%s: cfg=%v msg=%q", tc.name, cfg, msg)
		}
	}
	// required junction column outside the two link columns
	jr := junctionDef()
	jr.Columns = append(jr.Columns, defs.Column{Name: "note", FieldType: "text", Required: true})
	res2 := relsResolver()
	res2.tables["customer_tags"] = jr
	col := defs.Column{Name: "m2m_tags", FieldType: "m2m",
		M2M: &defs.M2M{JunctionTable: "customer_tags", SrcCol: "customer_id", TgtCol: "tag_id"}}
	if _, msg := ResolveM2M(res2, base, col); !strings.Contains(msg, "required column note outside the two link columns") {
		t.Fatalf("required-outside msg = %q", msg)
	}

	// happy path: refs and topology resolve
	cfg, msg := ResolveM2M(res, base, col)
	if cfg == nil || msg != "" {
		t.Fatalf("happy = %v %q", cfg, msg)
	}
	if cfg.Junction.Name != "customer_tags" || cfg.Target.Name != "tags" ||
		cfg.SrcCol != "customer_id" || cfg.TgtCol != "tag_id" ||
		cfg.SrcRef != "id" || cfg.TargetRef != "id" {
		t.Fatalf("cfg = %+v", cfg)
	}

	// junction fk targeting the junction itself resolves to the junction
	jr2 := junctionDef()
	jr2.Columns[1].FK = &defs.FK{Table: "", RefColumn: "customer_id"}
	res3 := relsResolver()
	res3.tables["customer_tags"] = jr2
	cfg, _ = ResolveM2M(res3, base, col)
	if cfg == nil || cfg.Target.Name != "customer_tags" || cfg.TargetRef != "customer_id" {
		t.Fatalf("self-target cfg = %+v", cfg)
	}
}

func TestM2MLinks(t *testing.T) {
	res := relsResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers":
			return &fakeAdapter{
				fetchByKey: func(sc, tb string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
					if fmt.Sprint(kv[0]) == "1" {
						return []map[string]any{{"id": 1.0, "name": "jo"}}, nil
					}
					if fmt.Sprint(kv[0]) == "2" {
						return []map[string]any{{"id": 2.0, "name": "ana", "note": nil}}, nil
					}
					return nil, nil
				},
			}, nil
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb, col, retCol string, vals []any) ([]ds.Pair, error) {
					return []ds.Pair{{Col: 1.0, Ret: 10.0}, {Col: 1.0, Ret: 11.0}}, nil
				},
			}, nil
		case "tags":
			return &fakeAdapter{
				fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
					return map[string]map[string]any{
						"10": {"id": 10.0, "label": "vip"},
						"11": {"id": 11.0, "label": "beta"},
					}, nil
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}
	svc := &ReadService{R: res}

	w := doRels(svc, "m2m", res.tables["customers"], "m2m_tags", EncodeRowKey([]string{"1"}), "")
	if w.Code != 200 {
		t.Fatalf("links = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"values":[10,11]`) ||
		!strings.Contains(body, `"label":"vip"`) || !strings.Contains(body, `"label":"beta"`) {
		t.Fatalf("links body = %s", body)
	}

	// row not found / bad key / column not found
	if w = doRels(svc, "m2m", res.tables["customers"], "m2m_tags", EncodeRowKey([]string{"9"}), ""); w.Code != 404 {
		t.Fatalf("missing row = %d", w.Code)
	}
	if w = doRels(svc, "m2m", res.tables["customers"], "m2m_tags", "notakey", ""); w.Code != 400 {
		t.Fatalf("bad key = %d", w.Code)
	}
	if w = doRels(svc, "m2m", res.tables["customers"], "name", EncodeRowKey([]string{"1"}), ""); w.Code != 404 {
		t.Fatalf("non-m2m col = %d", w.Code)
	}

	// no junction read grant → 403 (src row fetchable, links withheld)
	denied := &ReadService{R: res, CanRead: func(name string) bool { return name != "customer_tags" }}
	if w = doRels(denied, "m2m", res.tables["customers"], "m2m_tags", EncodeRowKey([]string{"1"}), ""); w.Code != 403 {
		t.Fatalf("no junction grant = %d %s", w.Code, w.Body)
	}

	// junction pair fetch failure → 502 links fetch failed
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers":
			return &fakeAdapter{
				fetchByKey: func(sc, tb string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
					return []map[string]any{{"id": 1.0, "name": "jo"}}, nil
				},
			}, nil
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb, col, retCol string, vals []any) ([]ds.Pair, error) {
					return nil, fmt.Errorf("boom")
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}
	if w = doRels(svc, "m2m", res.tables["customers"], "m2m_tags", EncodeRowKey([]string{"1"}), ""); w.Code != 502 ||
		!strings.Contains(w.Body.String(), "links fetch failed") {
		t.Fatalf("pair fetch failure = %d %s", w.Code, w.Body)
	}
}

func TestM2MOptions(t *testing.T) {
	res := relsResolver()
	res.tables["tags"].PageSize = 20
	var gotLP ds.ListParams
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		if tb.Name != "tags" {
			return nil, fmt.Errorf("unexpected table %s", tb.Name)
		}
		return &fakeAdapter{
			listRows: func(p ds.ListParams) ([]map[string]any, error) {
				gotLP = p
				return []map[string]any{{"id": 10.0, "label": "vip"}}, nil
			},
			countRows: func(p ds.ListParams) (int, error) { return 1, nil },
		}, nil
	}
	svc := &ReadService{R: res}

	w := doRels(svc, "m2moptions", res.tables["customers"], "m2m_tags", "", "?search=vi&page=2")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"label":"vip"`) ||
		!strings.Contains(w.Body.String(), `"total":1`) || !strings.Contains(w.Body.String(), `"page":2`) {
		t.Fatalf("m2moptions = %d %s", w.Code, w.Body)
	}
	// ref column first, display columns after, page offset from target pageSize
	if gotLP.Search != "vi" || gotLP.Columns[0] != "id" || gotLP.Offset != 20 {
		t.Fatalf("list params = %+v", gotLP)
	}

	// no junction grant → 403
	denied := &ReadService{R: res, CanRead: func(name string) bool { return name != "tags" }}
	if w = doRels(denied, "m2moptions", res.tables["customers"], "m2m_tags", "", "?search=vi"); w.Code != 403 {
		t.Fatalf("no target grant = %d %s", w.Code, w.Body)
	}
	// non-m2m column → 404
	if w = doRels(svc, "m2moptions", res.tables["customers"], "name", "", "?search=vi"); w.Code != 404 {
		t.Fatalf("non-m2m col = %d", w.Code)
	}
	// broken junction → 400 VALIDATION with the resolution message
	broken := customersDef()
	broken.Columns[2].M2M.JunctionTable = "customers"
	if w = doRels(svc, "m2moptions", broken, "m2m_tags", "", "?search=vi"); w.Code != 400 ||
		!strings.Contains(w.Body.String(), "junction cannot be this table itself") {
		t.Fatalf("broken junction = %d %s", w.Code, w.Body)
	}
}

func TestFKOptions(t *testing.T) {
	res := relsResolver()
	var gotLP ds.ListParams
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{
			listRows: func(p ds.ListParams) ([]map[string]any, error) {
				gotLP = p
				return []map[string]any{{"id": 7.0, "name": "jo"}}, nil
			},
			countRows: func(p ds.ListParams) (int, error) { return 1, nil },
		}, nil
	}
	svc := &ReadService{R: res}

	w := doRels(svc, "fkoptions", res.tables["orders"], "customer_id", "", "?search=jo")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"jo"`) ||
		!strings.Contains(w.Body.String(), `"pageSize":2`) {
		t.Fatalf("fkoptions = %d %s", w.Code, w.Body)
	}
	if gotLP.Columns[0] != "id" || gotLP.Search != "jo" {
		t.Fatalf("fk list params = %+v", gotLP)
	}

	// self-referencing fk lists from the source table itself (no CanRead hop)
	w = doRels(svc, "fkoptions", res.tables["orders"], "parent_id", "", "")
	if w.Code != 200 {
		t.Fatalf("self fkoptions = %d %s", w.Code, w.Body)
	}

	// no target grant → 403
	denied := &ReadService{R: res, CanRead: func(name string) bool { return name != "customers" }}
	if w = doRels(denied, "fkoptions", res.tables["orders"], "customer_id", "", ""); w.Code != 403 {
		t.Fatalf("no target grant = %d %s", w.Code, w.Body)
	}
	// non-fk column → 404
	if w = doRels(svc, "fkoptions", res.tables["orders"], "note", "", ""); w.Code != 404 {
		t.Fatalf("non-fk col = %d", w.Code)
	}
	// unresolvable target → 404 table def not found
	res.tables["orders"].Columns[2].FK.Table = "ghost"
	if w = doRels(svc, "fkoptions", res.tables["orders"], "customer_id", "", ""); w.Code != 404 ||
		!strings.Contains(w.Body.String(), "table def not found") {
		t.Fatalf("ghost target = %d %s", w.Code, w.Body)
	}
}
