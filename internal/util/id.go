// Package util holds small cross-cutting helpers.
package util

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a short prefixed identifier like "srv_3f9a2b1c".
func NewID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// rand should not fail; fall back to a fixed-but-unique-ish value.
		return prefix + "_00000000"
	}
	return prefix + "_" + hex.EncodeToString(b)
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous 0/O/1/I

// NewShareCode returns an uppercase 5-character code for tunnel sharing,
// drawn from an unambiguous alphabet.
func NewShareCode() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "MCOS5"
	}
	out := make([]byte, 5)
	for i, v := range b {
		out[i] = codeAlphabet[int(v)%len(codeAlphabet)]
	}
	return string(out)
}
