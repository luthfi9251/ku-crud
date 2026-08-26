package engine

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
)

func statsDef() *defs.Table {
	return &defs.Table{Name: "orders", Schema: "public", PhysTab: "orders",
		Keys: []string{"id"}, PageSize: 20, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Visible: true, Position: 0},
			{Name: "amount", Label: "Amount", FieldType: "number", Visible: true, Position: 1},
			{Name: "created", Label: "Created", FieldType: "datetime", Visible: true, Position: 2},
			{Name: "note", Label: "Note", FieldType: "text", Visible: true, Position: 3},
			{Name: "total", Label: "Total", FieldType: "number", IsComputed: true, Visible: true, Position: 4},
			{Name: "tags", Label: "Tags", FieldType: "m2m", Visible: true, Position: 5},
		}}
}

func doStats(svc *ReadService, target string, t *defs.Table) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	svc.Stats(w, req, t)
	return w
}

func TestStatsValidation(t *testing.T) {
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) { return &fakeAdapter{}, nil }}}
	td := statsDef()
	for _, tc := range []struct{ q, code string }{
		{"", "STATS_INVALID"},
		{"?func=median&column=amount", "STATS_INVALID"},
		{"?func=count&column=amount", "STATS_INVALID"},
		{"?func=sum", "STATS_INVALID"},
		{"?func=sum&column=note", "STATS_INVALID"},
		{"?func=avg&column=created", "STATS_INVALID"},
		{"?func=sum&column=nope", "STATS_INVALID"},
		{"?func=sum&column=total", "STATS_INVALID"}, // computed
		{"?func=min&column=tags", "STATS_INVALID"},  // m2m
		{"?func=count&filters=bad", "FILTER_INVALID"},
	} {
		w := doStats(svc, "/stats"+tc.q, td)
		var e map[string]any
		json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != 400 || e["code"] != tc.code {
			t.Errorf("%q = %d %v, want 400 %s", tc.q, w.Code, e["code"], tc.code)
		}
	}
}

func TestStatsTableMode(t *testing.T) {
	var got ds.AggregateParams
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(p ds.AggregateParams) (*ds.AggregateResult, error) {
				got = p
				return &ds.AggregateResult{Value: "123.45", HasRows: true}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=sum&column=amount&filters="+url.QueryEscape(`[{"column":"amount","op":"gt","values":["10"]}]`), statsDef())
	if w.Code != 200 {
		t.Fatalf("stats = %d %s", w.Code, w.Body)
	}
	var out struct {
		Func    string `json:"func"`
		Column  string `json:"column"`
		Value   any    `json:"value"`
		HasRows bool   `json:"hasRows"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Func != "sum" || out.Column != "amount" || out.Value != 123.45 || !out.HasRows {
		t.Fatalf("out = %s", w.Body)
	}
	if got.Schema != "public" || got.Table != "orders" || got.Query != "" || got.Func != "sum" || got.Column != "amount" {
		t.Fatalf("params = %+v", got)
	}
	if len(got.Filters) != 1 || got.Filters[0].Column != "amount" || got.Filters[0].Op != "gt" || got.Filters[0].Values[0] != 10.0 {
		t.Fatalf("filters = %+v", got.Filters)
	}
}

func TestStatsCountPassthrough(t *testing.T) {
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(ds.AggregateParams) (*ds.AggregateResult, error) {
				return &ds.AggregateResult{Value: int64(7), HasRows: true}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=count", statsDef())
	if w.Code != 200 {
		t.Fatalf("count = %d %s", w.Code, w.Body)
	}
	var out struct {
		Value any `json:"value"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Value != float64(7) { // JSON number
		t.Fatalf("value = %#v", out.Value)
	}
}

func TestStatsQueryMode(t *testing.T) {
	var got ds.AggregateParams
	qd := statsDef()
	qd.SourceType = "query"
	qd.QuerySQL = "SELECT amount FROM orders"
	qd.PhysTab = ""
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": qd},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(p ds.AggregateParams) (*ds.AggregateResult, error) {
				got = p
				return &ds.AggregateResult{Value: nil, HasRows: false}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=avg&column=amount", qd)
	if w.Code != 200 {
		t.Fatalf("stats = %d %s", w.Code, w.Body)
	}
	if got.Query == "" || got.Table != "" {
		t.Fatalf("params = %+v", got)
	}
	var out struct {
		Value   any  `json:"value"`
		HasRows bool `json:"hasRows"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Value != nil || out.HasRows {
		t.Fatalf("null agg = %s", w.Body)
	}
}

func TestStatsDatetimeMin(t *testing.T) {
	svc := &ReadService{R: &fakeResolver{tables: map[string]*defs.Table{"orders": statsDef()},
		adapter: func(*defs.Table) (ds.Adapter, error) {
			return &fakeAdapter{aggregate: func(ds.AggregateParams) (*ds.AggregateResult, error) {
				return &ds.AggregateResult{Value: "2026-01-02T03:04:05Z", HasRows: true}, nil
			}}, nil
		}}}
	w := doStats(svc, "/stats?func=min&column=created", statsDef())
	if w.Code != 200 {
		t.Fatalf("min = %d %s", w.Code, w.Body)
	}
	var out struct {
		Value any `json:"value"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Value != "2026-01-02T03:04:05Z" { // datetime stays a string
		t.Fatalf("value = %#v", out.Value)
	}
}
