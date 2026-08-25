package hooks

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryNilSafe(t *testing.T) {
	var r *Registry
	if _, ok := r.Get("x"); ok {
		t.Fatal("nil registry Get should miss")
	}
	if names := r.Names(); len(names) != 0 {
		t.Fatalf("nil registry Names = %v", names)
	}
}

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	fn := func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		return row, nil
	}
	if err := r.Register("a", fn); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("a", fn); err == nil {
		t.Fatal("duplicate register should fail")
	}
	if got := r.Names(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("Names = %v", got)
	}
	if _, ok := r.Get("a"); !ok {
		t.Fatal("Get(a) should hit")
	}
	if _, ok := r.Get("b"); ok {
		t.Fatal("Get(b) should miss")
	}
}

func TestParseAssignments(t *testing.T) {
	a, err := ParseAssignments("")
	if err != nil || len(a) != 0 {
		t.Fatalf("empty = %v %v", a, err)
	}
	in := `{"beforeCreate":[{"hook":"b","order":2},{"hook":"a","order":1}],
		"afterDelete":[{"hook":"c","config":{"k":1},"order":1}]}`
	a, err = ParseAssignments(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := a[BeforeCreate]; len(got) != 2 || got[0].Hook != "a" || got[1].Hook != "b" {
		t.Fatalf("beforeCreate not order-sorted: %v", got)
	}
	var cfg map[string]any
	json.Unmarshal(a[AfterDelete][0].Config, &cfg)
	if cfg["k"] != float64(1) {
		t.Fatalf("config not carried: %s", a[AfterDelete][0].Config)
	}
	names := map[string]bool{}
	for _, n := range a.Names() {
		names[n] = true
	}
	if !names["a"] || !names["b"] || !names["c"] || len(names) != 3 {
		t.Fatalf("Names = %v", names)
	}
	// equal orders keep list order (stable)
	a, _ = ParseAssignments(`{"beforeCreate":[{"hook":"x","order":1},{"hook":"y","order":1}]}`)
	if a[BeforeCreate][0].Hook != "x" {
		t.Fatal("equal orders must be stable")
	}
}

func TestParseAssignmentsErrors(t *testing.T) {
	if _, err := ParseAssignments(`{`); err == nil {
		t.Fatal("bad JSON must error")
	}
	if _, err := ParseAssignments(`{"beforeUpsert":[]}`); err == nil {
		t.Fatal("unknown event must error")
	}
	if _, err := ParseAssignments(`{"beforeCreate":[{"hook":""}]}`); err == nil {
		t.Fatal("empty hook name must error")
	}
}
