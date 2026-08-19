// Package tokenid encodes numeric metadata ids as short, opaque, unforgeable
// tokens for use in URLs and API payloads. Auto-increment ids are guessable
// and editable by clients (security misconfiguration surface); tokens hide
// them without growing (11 chars).
//
// Construction: a 4-round Feistel permutation over the 64-bit id keyed by
// HMAC-SHA256(secret). Deterministic (no nonce — output stays 11 chars),
// length-preserving and reversible; the HMAC keyed round function makes the
// permutation unforgeable and non-invertible without the secret. The entity
// kind ("ds", "td", "user", ...) is mixed into every round so a token from
// one kind decodes to an unrelated id under another kind.
package tokenid

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
)

const rounds = 4

type Codec struct {
	secret []byte
}

func New(secret []byte) *Codec { return &Codec{secret: secret} }

// round derives a 32-bit round function value: F(kind, i, r).
func (c *Codec) round(kind string, i int, r uint32) uint32 {
	mac := hmac.New(sha256.New, c.secret)
	var ib [1]byte
	ib[0] = byte(i)
	var rb [4]byte
	binary.BigEndian.PutUint32(rb[:], r)
	mac.Write([]byte(kind))
	mac.Write([]byte{0x1f}) // kind/value separator
	mac.Write(ib[:])
	mac.Write(rb[:])
	sum := mac.Sum(nil)
	return binary.BigEndian.Uint32(sum[:4])
}

func (c *Codec) permute(kind string, x uint64) uint64 {
	l, r := uint32(x>>32), uint32(x)
	for i := 0; i < rounds; i++ {
		l, r = r, l^c.round(kind, i, r)
	}
	return uint64(l)<<32 | uint64(r)
}

func (c *Codec) unpermute(kind string, x uint64) uint64 {
	l, r := uint32(x>>32), uint32(x)
	for i := rounds - 1; i >= 0; i-- {
		l, r = r^c.round(kind, i, l), l
	}
	return uint64(l)<<32 | uint64(r)
}

// Encode returns the 11-char base64url token for (kind, id).
func (c *Codec) Encode(kind string, id int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], c.permute(kind, uint64(id)))
	return base64.RawURLEncoding.EncodeToString(buf[:])
}

// Decode reverses Encode. Invalid encodings error; a well-formed token from
// a different kind or secret decodes to an unrelated id (callers treat it as
// not-found), never to an attacker-chosen id.
func (c *Codec) Decode(kind, token string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("bad token: %w", err)
	}
	if len(raw) != 8 {
		return 0, errors.New("bad token length")
	}
	id := c.unpermute(kind, binary.BigEndian.Uint64(raw))
	if id < 0 {
		return 0, errors.New("bad token value")
	}
	return int64(id), nil
}
