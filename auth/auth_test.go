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
func TestHMACAuth_OldTwoPartFormatRejected(t *testing.T) {
	a := newTestAuth()
	// Craft a fake 2-part token (old format). It has correct structure but must be rejected.
	legacyCookie := "9999999999.deadbeef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r.AddCookie(&http.Cookie{Name: "panel_admin", Value: legacyCookie})
	if a.Verified(r) {
		t.Error("legacy 2-part cookie format must be rejected by Verified()")
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
