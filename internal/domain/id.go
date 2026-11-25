package domain

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random 32-character hex id (UUID-sized, no dependency).
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}
