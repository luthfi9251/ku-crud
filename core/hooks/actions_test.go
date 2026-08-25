package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestParseActionsEmpty(t *testing.T) {
	cfg, err := ParseActions("")
	if err != nil || len(cfg.Hidden) != 0 || len(cfg.Custom) != 0 {
		t.Fatalf("empty = %+v %v", cfg, err)
	}
}

func TestParseActionsValid(t *testing.T) {
	in := `{"hidden":["refresh","copy"],"custom":[
		{"id":"a2","label":"B","grant":"update","hook":"H2","order":2},
		{"id":"a1","label":"A","confirm":"Sure?","grant":"read","hook":"H1","config":{"x":1},"order":1,"style":"danger"}]}`
	cfg, err := ParseActions(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hidden) != 2 || cfg.Hidden[0] != "refresh" {
		t.Fatalf("hidden = %v", cfg.Hidden)
	}
	// sorted by order
	if cfg.Custom[0].ID != "a1" || cfg.Custom[1].ID != "a2" {
		t.Fatalf("order = %+v", cfg.Custom)
	}
	// empty style defaults to neutral
	if cfg.Custom[1].Style != "neutral" {
		t.Fatalf("style default = %q", cfg.Custom[1].Style)
	}
	if cfg.Find("a2") == nil || cfg.Find("nope") != nil {
		t.Fatal("Find broken")
	}
}

func TestParseActionsErrors(t *testing.T) {
	for _, in := range []string{
		`{`,                   // bad json
		`{"hidden":["nope"]}`, // unknown hidden key
		`{"custom":[{"id":"a","label":"A","grant":"read","hook":"H"},{"id":"a","label":"B","grant":"read","hook":"H"}]}`, // dup id
		`{"custom":[{"id":"bad id!","label":"A","grant":"read","hook":"H"}]}`,                                            // bad id
		`{"custom":[{"id":"a","label":" ","grant":"read","hook":"H"}]}`,                                                  // empty label
		`{"custom":[{"id":"a","label":"A","grant":"read"}]}`,                                                             // no hook
		`{"custom":[{"id":"a","label":"A","grant":"rw","hook":"H"}]}`,                                                    // bad grant
		`{"custom":[{"id":"a","label":"A","grant":"read","hook":"H","style":"loud"}]}`,                                   // bad style
	} {
		if _, err := ParseActions(in); err == nil {
			t.Fatalf("expected error for %s", in)
		}
	}
}

func TestParseAssignmentsRejectsOnAction(t *testing.T) {
	if _, err := ParseAssignments(`{"onAction":[{"hook":"H","order":1}]}`); err == nil {
		t.Fatal("onAction must not be a CRUD assignment event")
	}
}

func testHook(fn func(row RowPayload) (RowPayload, error)) HookFunc {
	return func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		return fn(row)
	}
}

func TestRunActionMessage(t *testing.T) {
	reg := NewRegistry()
	reg.Register("Say", testHook(func(row RowPayload) (RowPayload, error) {
		row.Message = "hello " + row.Values["name"].(string)
		return row, nil
	}))
	msg, err := reg.RunAction(context.Background(), &HookContext{},
		RowPayload{Values: map[string]any{"name": "nia"}},
		Assignment{Hook: "Say"})
	if err != nil || msg != "hello nia" {
		t.Fatalf("run = %q %v", msg, err)
	}
}

func TestRunActionMissing(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.RunAction(context.Background(), &HookContext{}, RowPayload{}, Assignment{Hook: "Ghost"})
	var me *MissingError
	if !errors.As(err, &me) || me.Name != "Ghost" {
		t.Fatalf("err = %v", err)
	}
	// nil registry is also missing, never a silent success
	if _, err := (*Registry)(nil).RunAction(context.Background(), &HookContext{}, RowPayload{}, Assignment{Hook: "X"}); !errors.As(err, &me) {
		t.Fatalf("nil registry err = %v", err)
	}
}

func TestRunActionErrorAndPanic(t *testing.T) {
	reg := NewRegistry()
	reg.Register("Boom", testHook(func(row RowPayload) (RowPayload, error) {
		return row, errors.New("no can do")
	}))
	if _, err := reg.RunAction(context.Background(), &HookContext{}, RowPayload{}, Assignment{Hook: "Boom"}); err == nil || err.Error() != "no can do" {
		t.Fatalf("err = %v", err)
	}
	reg.Register("Panicky", testHook(func(row RowPayload) (RowPayload, error) {
		panic("kaboom")
	}))
	if _, err := reg.RunAction(context.Background(), &HookContext{}, RowPayload{}, Assignment{Hook: "Panicky"}); err == nil {
		t.Fatal("panic must become an error")
	}
}

func TestRunActionConfigPassthrough(t *testing.T) {
	reg := NewRegistry()
	var got json.RawMessage
	reg.Register("Cfg", func(ctx context.Context, hc *HookContext, ev Event, row RowPayload, cfg json.RawMessage) (RowPayload, error) {
		got = cfg
		return row, nil
	})
	reg.RunAction(context.Background(), &HookContext{}, RowPayload{},
		Assignment{Hook: "Cfg", Config: json.RawMessage(`{"a":1}`)})
	if string(got) != `{"a":1}` {
		t.Fatalf("cfg = %s", got)
	}
}
