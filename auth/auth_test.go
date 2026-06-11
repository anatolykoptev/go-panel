package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
)

func newTestAuth() *auth.HMACAuth {
	return auth.NewHMACAuth(auth.HMACConfig{
		Username:   "admin",
		Password:   "secret",
		HMACKey:    []byte("test-hmac-key-32-bytes-long-here"),
		BasePath:   "/admin",
		SessionTTL: time.Hour,
		Secure:     false,
	})
}

func TestHMACAuth_LoginLogoutRoundTrip(t *testing.T) {
	a := newTestAuth()

	// POST with correct credentials
	body := strings.NewReader("username=admin&password=secret")
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	a.LoginHandler().ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "panel_admin=") {
		t.Fatalf("expected panel_admin cookie, got %q", cookie)
	}

	// Extract cookie value and verify session
	cookieVal := extractCookieValue(cookie, "panel_admin")
	r2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r2.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	if !a.Verified(r2) {
		t.Fatal("expected verified session")
	}
}

func TestHMACAuth_WrongCredentials_Returns401(t *testing.T) {
	a := newTestAuth()

	body := strings.NewReader("username=admin&password=wrong")
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	a.LoginHandler().ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHMACAuth_Verified_FalseWithoutCookie(t *testing.T) {
	a := newTestAuth()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	if a.Verified(r) {
		t.Fatal("expected false without cookie")
	}
}

func TestHMACAuth_Require_RedirectsUnauthenticated(t *testing.T) {
	a := newTestAuth()
	handler := a.Require(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/page", nil)
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected redirect, got %d", w.Code)
	}
	if !strings.HasSuffix(w.Header().Get("Location"), "/admin/login") {
		t.Errorf("expected login redirect, got %q", w.Header().Get("Location"))
	}
}

func TestHMACAuth_Require_HTMXReturns401WithHeader(t *testing.T) {
	a := newTestAuth()
	handler := a.Require(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/page", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for htmx, got %d", w.Code)
	}
	if !strings.HasSuffix(w.Header().Get("HX-Redirect"), "/admin/login") {
		t.Errorf("expected HX-Redirect, got %q", w.Header().Get("HX-Redirect"))
	}
}

func TestHMACAuth_Logout_ClearsCookie(t *testing.T) {
	a := newTestAuth()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/logout", nil)
	w := httptest.NewRecorder()

	a.LogoutHandler().ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("expected redirect, got %d", w.Code)
	}
	setCookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "Max-Age=0") {
		t.Errorf("expected Max-Age=0 for logout, got %q", setCookie)
	}
}

// TestNewHMACAuth_PanicsOnShortKey verifies that NewHMACAuth panics when the key is < 32 bytes.
func TestNewHMACAuth_PanicsOnShortKey(t *testing.T) {
	cases := []struct {
		name string
		key  []byte
	}{
		{"empty", []byte{}},
		{"nil", nil},
		{"short", []byte("short-key")},
		{"31bytes", []byte("31-bytes-key-just-one-too-short")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic for short HMACKey")
				}
			}()
			auth.NewHMACAuth(auth.HMACConfig{
				Username: "admin",
				Password: "secret",
				HMACKey:  tc.key,
				BasePath: "/admin",
			})
		})
	}
}

// TestNewHMACAuth_OKOnExactFloor verifies that NewHMACAuth succeeds with exactly 32 bytes.
func TestNewHMACAuth_OKOnExactFloor(t *testing.T) {
	// Must not panic.
	_ = auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("exactly-32-bytes-long-key-here!!"),
		BasePath: "/admin",
	})
}

// TestHMACAuth_PerLoginNonce verifies that two consecutive logins produce different cookie values.
func TestHMACAuth_PerLoginNonce(t *testing.T) {
	a := newTestAuth()

	login := func() string {
		body := strings.NewReader("username=admin&password=secret")
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", body)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		a.LoginHandler().ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("login failed, status=%d", w.Code)
		}
		return extractCookieValue(w.Header().Get("Set-Cookie"), "panel_admin")
	}

	v1 := login()
	v2 := login()

	if v1 == "" || v2 == "" {
		t.Fatal("expected non-empty cookie values")
	}
	if v1 == v2 {
		t.Errorf("same-user back-to-back logins must produce different cookie values (nonce); got %q both times", v1)
	}
}

// TestHMACAuth_OldTwoPartFormatRejected verifies that the legacy exp.HMAC cookie format is rejected.
//
// The legacy token carries a VALID v1 HMAC-SHA256(key, "admin|9999999999") signature under the
// test key "test-hmac-key-32-bytes-long-here" (verified independently via crypto/hmac). A v1
// parser would accept this token; the v2 parser must reject it purely on format grounds (only
// 2 dot-separated parts, not 3). Without a valid v1 signature the test is tautological — a
// garbage-sig cookie is rejected by both old and new parsers.
func TestHMACAuth_OldTwoPartFormatRejected(t *testing.T) {
	a := newTestAuth()
	// valid v1 sig = HMAC-SHA256("test-hmac-key-32-bytes-long-here", "admin|9999999999")
	// = 5d00c7ed8b01869123334abee7d0cb389419395ea0a5fcf6e9da82be38a59992
	legacyCookie := "9999999999.5d00c7ed8b01869123334abee7d0cb389419395ea0a5fcf6e9da82be38a59992"
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: legacyCookie})
	if a.Verified(r) {
		t.Error("legacy 2-part cookie format must be rejected by Verified()")
	}
}

// TestHMACAuth_Verified_KAT is a known-answer test for the v2 cookie format.
// It independently recomputes HMAC-SHA256(key, "admin|exp|nonce") for a fixed
// triple and asserts Verified() accepts it. A round-trip test alone would stay
// green if makeToken and Verified drifted to a new format together (e.g. field
// reorder or separator change); this independent recomputation catches that class.
// See csrf/csrf_test.go TestVerify_KAT for the same pattern on the CSRF token.
//
// Fixed triple:
//
//	key   = "test-hmac-key-32-bytes-long-here"
//	user  = "admin"
//	exp   = 1799999999 (far future; won't expire until 2027)
//	nonce = "aabbccddeeff00112233445566778899"
//	sig   = HMAC-SHA256(key, "admin|1799999999|aabbccddeeff00112233445566778899")
//	      = 63ab12f322f2669d1121c17f9e8c82b821497dc0897547125bac7b22817a21b3
func TestHMACAuth_Verified_KAT(t *testing.T) {
	a := newTestAuth()
	// The cookie is pre-computed — Verified() must accept it without calling makeToken.
	katCookie := "1799999999.aabbccddeeff00112233445566778899.63ab12f322f2669d1121c17f9e8c82b821497dc0897547125bac7b22817a21b3"
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: katCookie})
	if !a.Verified(r) {
		t.Errorf("KAT failed: Verified() rejected a correctly-formed v2 cookie; cookie=%q", katCookie)
	}
}

// TestHMACAuth_Verified_KAT_TamperedNonce verifies that mutating the nonce in the KAT
// cookie invalidates the MAC (catches nonce-not-in-MAC regressions).
func TestHMACAuth_Verified_KAT_TamperedNonce(t *testing.T) {
	a := newTestAuth()
	// Same sig as KAT but nonce changed → MAC must not match.
	tampered := "1799999999.0000000000000000000000000000bbbb.63ab12f322f2669d1121c17f9e8c82b821497dc0897547125bac7b22817a21b3"
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: tampered})
	if a.Verified(r) {
		t.Error("Verified() accepted a cookie with a tampered nonce — nonce must be covered by the MAC")
	}
}

// extractCookieValue parses the Set-Cookie header to get the named cookie value.
func extractCookieValue(setCookie, name string) string {
	prefix := name + "="
	idx := strings.Index(setCookie, prefix)
	if idx < 0 {
		return ""
	}
	rest := setCookie[idx+len(prefix):]
	end := strings.IndexAny(rest, ";,")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
