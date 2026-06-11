package csrf_test

import (
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
	if err := csrf.Verify(testKey, "sess123", ""); err != csrf.ErrMissing {
		t.Errorf("expected ErrMissing, got %v", err)
	}
}

func TestVerify_Expired(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", -time.Minute)
	if err := csrf.Verify(testKey, "sess123", tok); err != csrf.ErrExpired {
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
	if err != csrf.ErrInvalid && err != csrf.ErrExpired {
		t.Errorf("expected ErrInvalid or ErrExpired, got %v", err)
	}
}

func TestVerify_WrongSession(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	if err := csrf.Verify(testKey, "sess-different", tok); err != csrf.ErrInvalid {
		t.Errorf("expected ErrInvalid for wrong session, got %v", err)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	tok := csrf.Issue(testKey, "sess123", csrf.DefaultTTL)
	otherKey := []byte("other-csrf-key-32-bytes-long-xxx")
	if err := csrf.Verify(otherKey, "sess123", tok); err != csrf.ErrInvalid {
		t.Errorf("expected ErrInvalid for wrong key, got %v", err)
	}
}

func TestVerify_MalformedNoSep(t *testing.T) {
	err := csrf.Verify(testKey, "sess123", "nopipehere")
	if err != csrf.ErrInvalid {
		t.Errorf("expected ErrInvalid for malformed token, got %v", err)
	}
}

func TestVerify_MalformedBadExp(t *testing.T) {
	err := csrf.Verify(testKey, "sess123", "notanumber|abcdef")
	if err != csrf.ErrInvalid {
		t.Errorf("expected ErrInvalid for bad expiry, got %v", err)
	}
}
