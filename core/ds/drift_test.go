package ds

import (
	"testing"

	"github.com/luthfi9251/kucrud-core/defs"
)

func TestCompareDrift(t *testing.T) {
	defined := []defs.Column{
		{Name: "id", FieldType: "number"},
		{Name: "gone", FieldType: "text"},
		{Name: "retyped", FieldType: "text"},
	}
	live := []LiveColumn{
		{Name: "id", FieldType: "number"},
		{Name: "retyped", FieldType: "number"},
		{Name: "fresh", FieldType: "text"},
	}
	rep := CompareDrift(defined, live)
	if len(rep.Missing) != 1 || rep.Missing[0] != "gone" {
		t.Fatalf("missing=%v", rep.Missing)
	}
	if len(rep.Added) != 1 || rep.Added[0] != "fresh" {
		t.Fatalf("added=%v", rep.Added)
	}
	if len(rep.TypeChanged) != 1 || rep.TypeChanged[0] != "retyped" {
		t.Fatalf("typeChanged=%v", rep.TypeChanged)
	}
	if rep.Empty() {
		t.Fatal("report must not be empty")
	}
	// clean case
	rep = CompareDrift([]defs.Column{{Name: "id", FieldType: "number"}},
		[]LiveColumn{{Name: "id", FieldType: "number"}})
	if !rep.Empty() {
		t.Fatalf("expected empty, got %+v", rep)
	}
}

func TestCompareDriftUUIDJSON(t *testing.T) {
	defined := []defs.Column{
		{Name: "id", FieldType: "uuid"},
		{Name: "meta", FieldType: "json"},
	}
	live := []LiveColumn{
		{Name: "id", FieldType: "uuid"},
		{Name: "meta", FieldType: "json"},
	}
	if !CompareDrift(defined, live).Empty() {
		t.Fatal("uuid/json must compare as first-class types")
	}
	live[1].FieldType = "text"
	if rep := CompareDrift(defined, live); len(rep.TypeChanged) != 1 {
		t.Fatalf("json→text must drift: %+v", rep)
	}
}

func TestEffectiveType(t *testing.T) {
	c := defs.Column{FieldType: "fk", BaseType: "number"}
	if EffectiveType(c) != "number" {
		t.Fatalf("fk col should compare as base type, got %q", EffectiveType(c))
	}
	if EffectiveType(defs.Column{FieldType: "text"}) != "text" {
		t.Fatal("non-fk passthrough")
	}
	if EffectiveType(defs.Column{FieldType: "fk"}) != "fk" {
		t.Fatal("fk without base falls back to fk")
	}
}
