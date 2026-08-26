package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckMissing(t *testing.T) {
	r := NewRegistry()
	r.Register("ok", func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		return row, nil
	})
	asgs, _ := ParseAssignments(`{"beforeCreate":[{"hook":"ok"}],"afterCreate":[{"hook":"gone"}]}`)
	err := r.CheckMissing(asgs)
	var me *MissingError
	if !errors.As(err, &me) || me.Name != "gone" {
		t.Fatalf("expected MissingError(gone), got %v", err)
	}
	ok, _ := ParseAssignments(`{"beforeCreate":[{"hook":"ok"}]}`)
	if err := r.CheckMissing(ok); err != nil {
		t.Fatal(err)
	}
}

func TestRunBeforeMutationAndRejection(t *testing.T) {
	r := NewRegistry()
	r.Register("double", func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		row.Values["qty"] = row.Values["qty"].(float64) * 2
		return row, nil
	})
	r.Register("reject", func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		return row, errors.New("not allowed by policy")
	})
	asgs, _ := ParseAssignments(`{"beforeCreate":[{"hook":"double","order":1}]}`)
	out, err := r.RunBefore(context.Background(), BeforeCreate, asgs[BeforeCreate], nil,
		RowPayload{Values: map[string]any{"qty": 2.0}})
	if err != nil || out.Values["qty"] != 4.0 {
		t.Fatalf("mutate: %v %v", out, err)
	}
	asgs, _ = ParseAssignments(`{"beforeCreate":[{"hook":"reject","order":1}]}`)
	if _, err = r.RunBefore(context.Background(), BeforeCreate, asgs[BeforeCreate], nil,
		RowPayload{Values: map[string]any{}}); err == nil || err.Error() != "not allowed by policy" {
		t.Fatalf("reject: %v", err)
	}
}

func TestRunBeforeOrderAndConfig(t *testing.T) {
	r := NewRegistry()
	var calls []string
	mk := func(n string) HookFunc {
		return func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
			calls = append(calls, n+":"+string(cfg))
			return row, nil
		}
	}
	r.Register("second", mk("second"))
	r.Register("first", mk("first"))
	asgs, _ := ParseAssignments(`{"beforeCreate":[{"hook":"second","order":2},{"hook":"first","order":1,"config":{"x":1}}]}`)
	if _, err := r.RunBefore(context.Background(), BeforeCreate, asgs[BeforeCreate], nil, RowPayload{Values: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "first:") || !strings.Contains(calls[0], `"x":1`) {
		t.Fatalf("order/config: %v", calls)
	}
}

func TestRunBeforePanicAndTimeout(t *testing.T) {
	r := NewRegistry()
	r.Register("boom", func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		panic("kaboom")
	})
	r.Register("slow", func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		select {
		case <-ctx.Done():
			return row, ctx.Err()
		case <-time.After(30 * time.Second):
		}
		return row, nil
	})
	asgs, _ := ParseAssignments(`{"beforeCreate":[{"hook":"boom"}]}`)
	_, err := r.RunBefore(context.Background(), BeforeCreate, asgs[BeforeCreate], nil, RowPayload{})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("panic not caught: %v", err)
	}
	asgs, _ = ParseAssignments(`{"beforeCreate":[{"hook":"slow"}]}`)
	start := time.Now()
	_, err = r.RunBefore(context.Background(), BeforeCreate, asgs[BeforeCreate], nil, RowPayload{})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout not enforced: %v", err)
	}
	if time.Since(start) > BeforeTimeout+time.Second {
		t.Fatal("RunBefore overran its timeout")
	}
}

func TestRunOneNilRegistry(t *testing.T) {
	var r *Registry
	if err := r.RunOne(context.Background(), nil, AfterCreate, RowPayload{}, Assignment{Hook: "x"}); err != nil {
		t.Fatalf("nil registry RunOne = %v", err)
	}
	if _, err := r.RunBefore(context.Background(), BeforeCreate, []Assignment{{Hook: "x"}}, nil, RowPayload{}); err != nil {
		t.Fatalf("nil registry RunBefore = %v", err)
	}
}
