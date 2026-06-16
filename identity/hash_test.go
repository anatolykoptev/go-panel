package identity_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/anatolykoptev/go-panel/identity"
)

// TestHasherDeterminism locks the property that the same value + same pepper
// always yields the same HMAC. Falsifiability: if Hash were salted with a random
// nonce (non-deterministic), the two outputs would differ and this test fails —
// which would also break O(1) identity lookups (ADR-002).
func TestHasherDeterminism(t *testing.T) {
	h := newHasher(t, "pepper-32-bytes-aaaaaaaaaaaaaaaa")
	a := h.Hash([]byte("alice@example.com"))
	b := h.Hash([]byte("alice@example.com"))
	if !bytes.Equal(a, b) {
		t.Fatalf("Hash not deterministic: %x != %x", a, b)
	}
	if len(a) != sha256.Size {
		t.Fatalf("Hash length = %d, want %d (SHA-256)", len(a), sha256.Size)
	}
}

// TestHasherPepperSensitivity locks the property that a different pepper yields a
// different hash for the same value. Falsifiability: if Hash ignored the pepper
// (e.g. bare sha256.Sum256 instead of HMAC-with-pepper), both outputs would be
// equal and this test fails — that is exactly the ADR-002 rainbow-table hole.
func TestHasherPepperSensitivity(t *testing.T) {
	h1 := newHasher(t, "pepper-one-aaaaaaaaaaaaaaaaaaaaaa")
	h2 := newHasher(t, "pepper-two-bbbbbbbbbbbbbbbbbbbbbb")
	a := h1.Hash([]byte("alice@example.com"))
	b := h2.Hash([]byte("alice@example.com"))
	if bytes.Equal(a, b) {
		t.Fatal("different peppers produced identical hash — pepper is not load-bearing")
	}
}

// TestHasherIsHMACSHA256 pins the exact construction: HMAC-SHA256(value, pepper),
// not a bare SHA-256. ADR-002 requires crypto/hmac with sha256.New.
func TestHasherIsHMACSHA256(t *testing.T) {
	pepper := []byte("pepper-32-bytes-cccccccccccccccc")
	value := []byte("alice@example.com")
	h := newHasher(t, string(pepper))

	mac := hmac.New(sha256.New, pepper)
	mac.Write(value)
	want := mac.Sum(nil)

	if got := h.Hash(value); !bytes.Equal(got, want) {
		t.Fatalf("Hash = %x, want HMAC-SHA256 %x", got, want)
	}
}

// TestHasherFunc verifies the Hash method is assignable to Config.Hasher (a
// func([]byte) []byte), which is how the handlers consume it.
func TestHasherFunc(t *testing.T) {
	h := newHasher(t, "pepper-32-bytes-dddddddddddddddd")
	cfg := identity.Config{Hasher: h.Hash} // compiles only if the types match
	if len(cfg.Hasher([]byte("x"))) != sha256.Size {
		t.Fatal("Hasher func produced wrong-length output")
	}
}

func newHasher(t *testing.T, pepper string) *identity.ProviderUIDHasher {
	t.Helper()
	h, err := identity.NewProviderUIDHasher([]byte(pepper))
	if err != nil {
		t.Fatalf("NewProviderUIDHasher(%d bytes): %v", len(pepper), err)
	}
	return h
}

// TestHasherRejectsShortPepper locks SEC-CR-001: a pepper shorter than
// MinPepperLen (or empty/nil) is rejected at construction, not silently
// accepted. A weak pepper makes the keyed HMAC brute-forceable, enabling
// provider-uid forgery and rainbow-table recovery of the protected identifiers.
func TestHasherRejectsShortPepper(t *testing.T) {
	weak := [][]byte{nil, {}, []byte("short"), make([]byte, identity.MinPepperLen-1)}
	for _, pepper := range weak {
		if _, err := identity.NewProviderUIDHasher(pepper); err == nil {
			t.Fatalf("NewProviderUIDHasher accepted a weak %d-byte pepper; want error", len(pepper))
		}
	}
	if _, err := identity.NewProviderUIDHasher(make([]byte, identity.MinPepperLen)); err != nil {
		t.Fatalf("NewProviderUIDHasher rejected a %d-byte pepper: %v", identity.MinPepperLen, err)
	}
}
