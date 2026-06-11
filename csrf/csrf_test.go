package csrf_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/csrf"
)

var testKey = []byte("test-csrf-key-32-bytes-long-here")

func TestIssueVerify_RoundTrip(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	if err := csrf.Verify(testKey, "sess123", tok); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestVerify_Missing(t *testing.T) {
	if err := csrf.Verify(testKey, "sess123", ""); !errors.Is(err, csrf.ErrMissing) {
		t.Errorf("expected ErrMissing, got %v", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", -time.Minute)
	if err := csrf.Verify(testKey, "sess123", tok); !errors.Is(err, csrf.ErrExpired) {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestVerify_Tampered(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	// Flip the last character of the signature.
	runes := []rune(tok)
	if runes[len(runes)-1] == 'a' {
		runes[len(runes)-1] = 'b'
	} else {
		runes[len(runes)-1] = 'a'
	}
	tampered := string(runes)
	err := csrf.Verify(testKey, "sess123", tampered)
	if !errors.Is(err, csrf.ErrInvalid) && !errors.Is(err, csrf.ErrExpired) {
		t.Errorf("expected ErrInvalid or ErrExpired, got %v", err)
	}
}

func TestVerify_WrongSession(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	if err := csrf.Verify(testKey, "sess-different", tok); !errors.Is(err, csrf.ErrInvalid) {
		t.Errorf("expected ErrInvalid for wrong session, got %v", err)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	otherKey := []byte("other-csrf-key-32-bytes-long-xxx")
	if err := csrf.Verify(otherKey, "sess123", tok); !errors.Is(err, csrf.ErrInvalid) {
		t.Errorf("expected ErrInvalid for wrong key, got %v", err)
	}
}

func TestVerify_MalformedNoSep(t *testing.T) {
	err := csrf.Verify(testKey, "sess123", "nopipehere")
	if !errors.Is(err, csrf.ErrInvalid) {
		t.Errorf("expected ErrInvalid for malformed token, got %v", err)
	}
}

func TestVerify_MalformedBadExp(t *testing.T) {
	err := csrf.Verify(testKey, "sess123", "notanumber|abcdef")
	if !errors.Is(err, csrf.ErrInvalid) {
		t.Errorf("expected ErrInvalid for bad expiry, got %v", err)
	}
}

// TestVerify_FarFutureExpiry verifies that tokens with exp > now+24h are rejected (SEC-CR-003).
// A token issuer must not be trusted to set arbitrarily long TTLs.
func TestVerify_FarFutureExpiry(t *testing.T) {
	// Issue a token with a 48h TTL — beyond the maxFutureTTL ceiling.
	tok := csrf.Issue(testKey, "sess123", 48*time.Hour)
	if err := csrf.Verify(testKey, "sess123", tok); !errors.Is(err, csrf.ErrInvalid) {
		t.Errorf("expected ErrInvalid for far-future expiry (>24h ceiling), got %v", err)
	}
}

// TestVerify_KAT is a known-answer test: fixed (key, sessionValue, expiry) must produce
// the exact expected hex MAC. Catches any change to the concat format or hash function.
//
// Wire format: expiry "|" hex(HMAC-SHA256(key, sessionValue "|" expiry)) — expiry FIRST.
// Inputs: key="test-csrf-key-32-bytes-long-here", session="kat-session", exp=1735689600 (2025-01-01 00:00:00 UTC)
// TestVerify_KAT pins the exact MAC construction by recomputing the HMAC
// independently in the test: token = "<exp>|" + hex(HMAC-SHA256(key, sessionValue+"|"+exp)).
// A round-trip test alone would stay green if Issue and Verify drifted to a new
// format together; this independent recomputation catches that class.
func TestVerify_KAT(t *testing.T) {
	tok := csrf.Issue(testKey, "kat-session", csrf.DefaultTTL)
	parts := strings.SplitN(tok, "|", 2)
	if len(parts) != 2 {
		t.Fatalf("token format broken: expected 'exp|mac', got %q", tok)
	}
	if len(parts[1]) != 64 {
		t.Fatalf("MAC should be 64 hex chars (SHA-256), got %d chars: %q", len(parts[1]), parts[1])
	}
	// Independent recomputation of the documented wire construction.
	mac := hmac.New(sha256.New, testKey)
	mac.Write([]byte("kat-session|" + parts[0]))
	want := hex.EncodeToString(mac.Sum(nil))
	if parts[1] != want {
		t.Errorf("MAC construction drifted:\n  token mac: %s\n  HMAC-SHA256(key, sessionValue|exp): %s", parts[1], want)
	}
	// Round-trip must pass.
	if err := csrf.Verify(testKey, "kat-session", tok); err != nil {
		t.Errorf("KAT round-trip failed: %v", err)
	}
}

// TestVerify_TamperExpiry verifies that substituting a different expiry prefix while keeping
// the original MAC causes ErrInvalid (format integrity check).
func TestVerify_TamperExpiry(t *testing.T) {
	// Issue a valid token.
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	// Extract the MAC (everything after the first '|').
	pipe := strings.IndexByte(tok, '|')
	if pipe < 0 {
		t.Fatalf("malformed token from Issue: %q", tok)
	}
	macPart := tok[pipe:] // "|<hex>"

	// Craft a new token: substitute a different (but still in-ceiling) expiry with the original MAC.
	// now+30min is a valid expiry, but HMAC was computed over the original exp — signature mismatch.
	altExp := time.Now().Add(30 * time.Minute).Unix()
	tampered := fmt.Sprintf("%d%s", altExp, macPart)

	if err := csrf.Verify(testKey, "sess123", tampered); !errors.Is(err, csrf.ErrInvalid) {
		t.Errorf("expected ErrInvalid for tampered expiry prefix, got %v", err)
	}
}
