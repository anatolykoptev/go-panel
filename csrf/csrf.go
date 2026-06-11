// Package csrf provides double-submit CSRF protection for go-panel write forms.
//
// Token format: HMAC(key, sessionValue|expiry) + "|" + expiry (hex-encoded MAC).
// The token is issued as a hidden form input on GET and verified on POST.
// Comparison is constant-time (hmac.Equal) to prevent timing attacks.
//
// Key lifecycle: the CSRFKey is passed via resource.Config. An empty key with
// any Writer-enabled resource causes a panic at Register time (fail-closed).
//
// Usage:
//
//	token := csrf.Issue(key, sessionCookieValue, ttl)
//	// embed token in the form hidden input named "_csrf"
//	if err := csrf.Verify(key, sessionCookieValue, formToken); err != nil {
//	    http.Error(w, "invalid CSRF token", http.StatusForbidden)
//	    return
//	}
package csrf

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// FormField is the hidden input name for CSRF tokens.
	FormField = "_csrf"
	// DefaultTTL is the default CSRF token lifetime.
	DefaultTTL = 2 * time.Hour
)

var (
	// ErrMissing is returned when no token is present.
	ErrMissing = errors.New("csrf: token missing")
	// ErrExpired is returned when the token has expired.
	ErrExpired = errors.New("csrf: token expired")
	// ErrInvalid is returned when the token signature does not match.
	ErrInvalid = errors.New("csrf: token invalid")
)

// Issue generates a signed CSRF token bound to the session cookie value.
// The returned string is safe to embed in a hidden HTML input.
func Issue(key []byte, sessionValue string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	sig := sign(key, sessionValue, exp)
	return fmt.Sprintf("%d|%s", exp, sig)
}

// Verify checks the token against the session cookie value.
// Returns ErrMissing, ErrExpired, or ErrInvalid on failure; nil on success.
func Verify(key []byte, sessionValue, token string) error {
	if token == "" {
		return ErrMissing
	}
	sep := strings.IndexByte(token, '|')
	if sep < 0 {
		return ErrInvalid
	}
	exp, err := strconv.ParseInt(token[:sep], 10, 64)
	if err != nil {
		return ErrInvalid
	}
	if time.Now().Unix() > exp {
		return ErrExpired
	}
	got := token[sep+1:]
	expected := sign(key, sessionValue, exp)
	if !hmac.Equal([]byte(got), []byte(expected)) {
		return ErrInvalid
	}
	return nil
}

// sign returns the hex-encoded HMAC-SHA256 of "sessionValue|expiry".
func sign(key []byte, sessionValue string, exp int64) string {
	mac := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(mac, "%s|%d", sessionValue, exp)
	return hex.EncodeToString(mac.Sum(nil))
}
