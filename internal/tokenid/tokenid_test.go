package tokenid

import (
	"strings"
	"testing"
)

func newCodec() *Codec { return New([]byte("test-secret-0123456789abcdef")) }

func TestRoundtrip(t *testing.T) {
	c := newCodec()
	kinds := []string{"ds", "td", "user", "role", "audit"}
	for _, kind := range kinds {
		for _, id := range []int64{1, 2, 42, 999999, 1 << 40} {
			tok := c.Encode(kind, id)
			got, err := c.Decode(kind, tok)
			if err != nil {
				t.Fatalf("Decode(%q,%q): %v", kind, tok, err)
			}
			if got != id {
				t.Fatalf("kind %q: got %d want %d", kind, got, id)
			}
		}
	}
}

func TestTokenShape(t *testing.T) {
	c := newCodec()
	tok := c.Encode("td", 7)
	if len(tok) != 11 {
		t.Fatalf("token %q: want 11 chars, got %d", tok, len(tok))
	}
	if strings.ContainsAny(tok, "+/=") {
		t.Fatalf("token %q: must be base64url without padding", tok)
	}
}

func TestDeterministic(t *testing.T) {
	c := newCodec()
	a, b := c.Encode("ds", 123), c.Encode("ds", 123)
	if a != b {
		t.Fatalf("encode not deterministic: %q vs %q", a, b)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	c := newCodec()
	for _, bad := range []string{"", "abc", "not a token!!", "WyIzIl0=", "AAAAAAAAAAAAAAAA"} {
		if _, err := c.Decode("td", bad); err == nil {
			t.Fatalf("Decode(%q): expected error", bad)
		}
	}
}

func TestDomainSeparation(t *testing.T) {
	c := newCodec()
	tok := c.Encode("ds", 5)
	if got, err := c.Decode("td", tok); err != nil || got == 5 {
		t.Fatalf("token from kind ds decoded to same id under kind td: got %d err %v", got, err)
	}
}

func TestDistinctOverRange(t *testing.T) {
	c := newCodec()
	seen := map[string]int64{}
	for id := int64(0); id < 5000; id++ {
		tok := c.Encode("td", id)
		if prev, dup := seen[tok]; dup {
			t.Fatalf("collision: id %d and %d both encode to %q", prev, id, tok)
		}
		seen[tok] = id
	}
}
