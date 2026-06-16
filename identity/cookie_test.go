package identity_test

import (
	"net/http"
	"testing"

	"github.com/anatolykoptev/go-panel/identity"
)

const cookieHost = "piter.now"

// TestCookieExactHostNoDomain locks the operator/public isolation invariant: the
// session cookie has NO Domain attribute, so it binds to the exact host and is
// never sent to admin.piter.now. Falsifiability: setting Domain=".piter.now"
// (or any Domain) makes c.Domain non-empty and this test fails.
func TestCookieExactHostNoDomain(t *testing.T) {
	cfg := identity.DefaultCookieConfig()
	c := cfg.Build(cookieHost, "sid-value")
	if c.Domain != "" {
		t.Fatalf("cookie Domain = %q, want empty (exact-host binding)", c.Domain)
	}
}

func TestCookieSecurityAttributes(t *testing.T) {
	cfg := identity.DefaultCookieConfig()
	c := cfg.Build(cookieHost, "sid-value")

	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true (JS must not read the session cookie)")
	}
	if !c.Secure {
		t.Error("Secure = false, want true (cookie must be HTTPS-only)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Value != "sid-value" {
		t.Errorf("Value = %q, want sid-value", c.Value)
	}
	if c.Name == "" {
		t.Error("cookie Name is empty")
	}
}

// TestCookieExpire locks logout: the expiry cookie clears the value and sets a
// negative MaxAge so the browser deletes it. Falsifiability: a positive MaxAge or
// non-empty value would fail.
func TestCookieExpire(t *testing.T) {
	cfg := identity.DefaultCookieConfig()
	c := cfg.Expire(cookieHost)

	if c.MaxAge >= 0 {
		t.Fatalf("expire MaxAge = %d, want negative", c.MaxAge)
	}
	if c.Value != "" {
		t.Fatalf("expire Value = %q, want empty", c.Value)
	}
	if c.Name != cfg.Name {
		t.Fatalf("expire Name = %q, want %q", c.Name, cfg.Name)
	}
	if c.Domain != "" {
		t.Fatalf("expire Domain = %q, want empty", c.Domain)
	}
}
