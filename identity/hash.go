// Package identity is the reusable public-auth framework for go-panel. It wires
// pluggable providers (magic-link now), Redis-backed sessions, a pepper-keyed
// provider-uid hasher, exact-host cookies, and HTTP handler builders. It is pure
// library code: it imports nothing from go-grad. Concrete deps (UserStore over
// pgxpool, Redis client, SMTP creds, per-region pepper) are injected by go-grad.
//
// Security posture is documented in docs/identity-design.md and ADR-001/002.
package identity

import (
	"crypto/hmac"
	"crypto/sha256"
)

// ProviderUIDHasher computes HMAC-SHA256(value, pepper) for provider-uid storage.
//
// Per ADR-002, external identifiers (email, VK id) are small-keyspace and must
// never be stored in plaintext nor as a bare SHA-256 (rainbow-table reversible).
// The per-region pepper makes a DB dump useless without the key. The hash is
// deterministic so identity lookups stay O(1) index hits.
type ProviderUIDHasher struct {
	pepper []byte
}

// NewProviderUIDHasher returns a hasher keyed by pepper. The pepper is a secret
// (≥32 bytes recommended) injected by go-grad from AUTH_IDENTITY_PEPPER; it is
// never read from a package global (ADR-001/002).
func NewProviderUIDHasher(pepper []byte) *ProviderUIDHasher {
	// Copy so callers cannot mutate the key underneath us.
	cp := make([]byte, len(pepper))
	copy(cp, pepper)
	return &ProviderUIDHasher{pepper: cp}
}

// Hash returns HMAC-SHA256(value, pepper). The output is 32 bytes. The method
// value satisfies func([]byte) []byte so it can be passed as Config.Hasher.
func (h *ProviderUIDHasher) Hash(value []byte) []byte {
	mac := hmac.New(sha256.New, h.pepper)
	mac.Write(value)
	return mac.Sum(nil)
}
