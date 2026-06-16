package identity

import "net/http"

// DefaultCookieName is the public-auth session cookie. It is distinct from the
// operator-auth cookie: the two auth worlds share no cookie jar (design doc §2).
const DefaultCookieName = "pn_pub"

// CookieConfig describes how the session cookie is emitted. Defaults
// (HttpOnly, Secure, SameSite=Lax, no Domain) are security-load-bearing.
type CookieConfig struct {
	Name     string
	Path     string
	MaxAge   int
	Secure   bool
	HttpOnly bool
	SameSite http.SameSite
}

// DefaultCookieConfig returns the hardened default: HttpOnly + Secure + Lax,
// path "/", and no Domain attribute (exact-host binding). MaxAge defaults to 0
// (session cookie); go-grad may override it to the session TTL.
func DefaultCookieConfig() CookieConfig {
	return CookieConfig{
		Name:     DefaultCookieName,
		Path:     "/",
		MaxAge:   0,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// Build returns a *http.Cookie carrying value.
//
// The host argument names the host this cookie is being set for, but it is
// deliberately NOT written to the Domain attribute. Omitting Domain yields
// exact-host binding (RFC 6265 §4.1.2.3): the cookie is sent only to the origin
// host, never to sibling subdomains such as admin.piter.now. Setting
// Domain=piter.now would leak the public session cookie to the operator host.
func (c CookieConfig) Build(host, value string) *http.Cookie {
	_ = host // intentionally unused: exact-host binding requires omitting Domain.
	return &http.Cookie{
		Name:     c.Name,
		Value:    value,
		Path:     c.Path,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HttpOnly: c.HttpOnly,
		SameSite: c.SameSite,
		// Domain intentionally unset → exact-host.
	}
}

// Expire returns a cookie that deletes the session cookie (empty value, negative
// MaxAge), preserving Name/Path/security attributes and the no-Domain binding.
func (c CookieConfig) Expire(host string) *http.Cookie {
	ck := c.Build(host, "")
	ck.MaxAge = -1
	return ck
}
