package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
)

// These tests pin the pre-extraction behavior for dangling def references
// (a junction/fk target definition deleted after its dependents were
// saved): every site errors or skips exactly like the old id-based code —
// never silently resolving the dangling id to the source table ("" self
// convention) or the junction.

// driftResolver mirrors relsResolver with the tags def deleted: the
// junction's tag_id fk carries the sentinel.
func driftResolver() *fakeResolver {
	res := relsResolver()
	jr := junctionDef()
	jr.Columns[1].FK = &defs.FK{Table: defs.MissingTable, RefColumn: "id",
		DisplayColumns: []string{"label"}}
	res.tables["customer_tags"] = jr
	delete(res.tables, "tags")
	return res
}

func TestResolveM2MDanglingTarget(t *testing.T) {
	res := driftResolver()
	base := res.tables["customers"]
	col := base.Columns[2]
	cfg, msg := ResolveM2M(res, base, col)
	if cfg == nil || msg != "" {
		t.Fatalf("dangling target cfg = %v %q", cfg, msg)
	}
	if !cfg.TargetMissing || cfg.Target != nil {
		t.Fatalf("TargetMissing = %v Target = %v", cfg.TargetMissing, cfg.Target)
	}
	if cfg.Junction.Name != "customer_tags" || cfg.SrcCol != "customer_id" ||
		cfg.TgtCol != "tag_id" || cfg.SrcRef != "id" || cfg.TargetRef != "id" {
		t.Fatalf("topology = %+v", cfg)
	}

	// dangling junction def → old resolveM2M's GetTableDef failure message
	dang := customersDef()
	dang.Columns[2].M2M.JunctionTable = defs.MissingTable
	if cfg, msg := ResolveM2M(res, dang, dang.Columns[2]); cfg != nil ||
		msg != "column m2m_tags: junction definition not found (save it first)" {
		t.Fatalf("dangling junction = %v %q", cfg, msg)
	}
}

func TestM2MOptionsDanglingTarget(t *testing.T) {
	res := driftResolver()
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		// the old flow never listed anything once the target def lookup
		// failed — not even junction rows
		return nil, fmt.Errorf("unexpected list on %s", tb.Name)
	}
	// grant-ful caller (admin): old GetTableDef failure → 404
	svc := &ReadService{R: res}
	w := doRels(svc, "m2moptions", res.tables["customers"], "m2m_tags", "", "")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "table def not found") {
		t.Fatalf("admin dangling target = %d %s", w.Code, w.Body)
	}
	// grant-less caller: old hasTablePerm on the dangling id → 403
	denied := &ReadService{R: res, CanRead: func(name string) bool {
		return name != defs.MissingTable
	}}
	if w = doRels(denied, "m2moptions", res.tables["customers"], "m2m_tags", "", ""); w.Code != 403 ||
		!strings.Contains(w.Body.String(), "no read access to the related tables") {
		t.Fatalf("non-admin dangling target = %d %s", w.Code, w.Body)
	}
}

func TestM2MLinksDanglingTarget(t *testing.T) {
	res := driftResolver()
	pairsFetched := false
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers":
			return &fakeAdapter{
				fetchByKey: func(sc, tb2 string, kc []string, kv []any, cols []string) ([]map[string]any, error) {
					return []map[string]any{{"id": 1.0, "name": "jo"}}, nil
				},
			}, nil
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb2, col, retCol string, vals []any) ([]ds.Pair, error) {
					pairsFetched = true
					return []ds.Pair{{Col: 1.0, Ret: 10.0}}, nil
				},
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}
	// grant-ful caller: old m2mLinks fetched the junction links, then the
	// target def lookup failed → 502 links fetch failed
	svc := &ReadService{R: res}
	w := doRels(svc, "m2m", res.tables["customers"], "m2m_tags", EncodeRowKey([]string{"1"}), "")
	if w.Code != 502 || !strings.Contains(w.Body.String(), "links fetch failed") {
		t.Fatalf("admin dangling target = %d %s", w.Code, w.Body)
	}
	if !pairsFetched {
		t.Fatal("junction links were not fetched before the 502 (old flow fetched them)")
	}
	// grant-less caller → 403 before any junction fetch of links
	denied := &ReadService{R: res, CanRead: func(name string) bool {
		return name != defs.MissingTable
	}}
	if w = doRels(denied, "m2m", res.tables["customers"], "m2m_tags", EncodeRowKey([]string{"1"}), ""); w.Code != 403 {
		t.Fatalf("non-admin dangling target = %d %s", w.Code, w.Body)
	}
}

func TestFKOptionsDanglingTarget(t *testing.T) {
	res := relsResolver()
	orders := res.tables["orders"]
	orders.Columns[2].FK.Table = defs.MissingTable
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return nil, fmt.Errorf("unexpected list on %s", tb.Name)
	}
	// grant-ful caller: old perm passed, def lookup failed → 404
	svc := &ReadService{R: res}
	w := doRels(svc, "fkoptions", orders, "customer_id", "", "")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "table def not found") {
		t.Fatalf("admin dangling fk = %d %s", w.Code, w.Body)
	}
	// grant-less caller → 403
	denied := &ReadService{R: res, CanRead: func(name string) bool {
		return name != defs.MissingTable
	}}
	if w = doRels(denied, "fkoptions", orders, "customer_id", "", ""); w.Code != 403 ||
		!strings.Contains(w.Body.String(), "no read access to the related table") {
		t.Fatalf("non-admin dangling fk = %d %s", w.Code, w.Body)
	}
}

func TestListDanglingRefsSkipSilently(t *testing.T) {
	res := driftResolver()
	orders := res.tables["orders"]
	orders.Columns[2].FK.Table = defs.MissingTable // dangling fk
	listed := map[string]bool{}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers", "orders":
			return &fakeAdapter{
				listRows: func(p ds.ListParams) ([]map[string]any, error) {
					listed[tb.Name] = true
					if tb.Name == "customers" {
						return []map[string]any{{"id": 1.0, "name": "jo"}}, nil
					}
					return []map[string]any{{"id": 1.0, "note": "n", "customer_id": 7.0}}, nil
				},
				countRows: func(p ds.ListParams) (int, error) { return 1, nil },
			}, nil
		}
		return nil, fmt.Errorf("unexpected table %s", tb.Name)
	}
	svc := &ReadService{R: res}

	// fk list: dangling fk target skipped, page still 200
	w := doReadT(svc, "GET", "/rows", "", orders)
	if w.Code != 200 {
		t.Fatalf("fk list = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), `"rels":{"customer_id"`) {
		t.Fatalf("dangling fk rendered rels: %s", w.Body)
	}
	if !listed["orders"] {
		t.Fatal("source page not listed")
	}

	// m2m list: dangling target skipped, page still 200
	w = doReadT(svc, "GET", "/rows", "", res.tables["customers"])
	if w.Code != 200 {
		t.Fatalf("m2m list = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), `"m2mRels":{"m2m_tags"`) {
		t.Fatalf("dangling m2m rendered rels: %s", w.Body)
	}
	// the junction must not be queried for display rows either
	if listed["customer_tags"] {
		t.Fatal("junction queried for m2m display rows")
	}
}

func TestWriteFKDanglingTarget(t *testing.T) {
	res := writeResolver()
	cust := res.tables["customers"]
	cust.Columns[2].FK.Table = defs.MissingTable
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{}, nil
	}
	svc := &WriteService{R: res}
	// old checkFKValues: def lookup failure → "fk target unavailable",
	// surfaced as 502 reference check failed (never a self-resolution
	// check against the source table)
	w := doWrite(svc, "POST", "", `{"id":1,"name":"jo","region_id":5}`, cust)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "reference check failed") ||
		!strings.Contains(w.Body.String(), "region_id: fk target unavailable") {
		t.Fatalf("dangling fk write = %d %s", w.Code, w.Body)
	}
}

func TestWriteM2MDanglingTargetStillSyncs(t *testing.T) {
	// the old write path never resolved the m2m target def — link sync
	// stays junction-only and succeeds with a dangling target
	res := driftResolver()
	cust := res.tables["customers"]
	var jInserts, jDeletes [][2]any
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customer_tags":
			return &fakeAdapter{
				fetchPairs: func(sc, tb2, col, retCol string, vals []any) ([]ds.Pair, error) {
					return []ds.Pair{{Col: int64(1), Ret: float64(2)}}, nil
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
	svc := &WriteService{R: res}
	w := doWrite(svc, "PUT", EncodeRowKey([]string{"1"}), `{"name":"jo","m2m_tags":[1,3]}`, cust)
	if w.Code != 200 {
		t.Fatalf("m2m sync with dangling target = %d %s", w.Code, w.Body)
	}
	if len(jInserts) != 2 || jInserts[0][1] != float64(1) || jInserts[1][1] != float64(3) {
		t.Fatalf("junction inserts = %v", jInserts)
	}
	if len(jDeletes) != 1 || jDeletes[0][1] != float64(2) {
		t.Fatalf("junction deletes = %v", jDeletes)
	}
}
