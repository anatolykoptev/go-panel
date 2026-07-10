package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anatolykoptev/go-panel/shell"
)

// BcryptTOTPAuth is a multi-user, bcrypt-password session authenticator backed by
// an AccountStore. It implements Authenticator. TOTP second-factor is planned and
// not yet wired; the type name reflects the target shape. Ported in design from
// oxpulse-admin/internal/admin/auth.go.
type BcryptTOTPAuth struct {
	store                AccountStore
	hmacKey              []byte
	basePath             string
	cookieName           string
	sessionTTL           time.Duration
	secure               bool
	loginTempl           func(errMsg string) http.Handler
	observer             Observer
	revocationFailClosed bool
	rateLimiter          RateLimiter
	loginRate            RateRule
	clientIP             func(*http.Request) string
}

// BcryptConfig configures BcryptTOTPAuth.
type BcryptConfig struct {
	// Store is the account persistence seam (required).
	Store AccountStore
	// HMACKey signs session cookies (required, >= 32 bytes).
	HMACKey []byte
	// BasePath is the admin prefix; default "/admin".
	BasePath string
	// CookieName overrides the session cookie name; default "panel_admin".
	CookieName string
	// SessionTTL is the session lifetime; default 24h.
	SessionTTL time.Duration
	// Secure sets the Secure cookie flag (false only for local-dev http).
	Secure bool
	// LoginTempl optionally overrides the login page.
	LoginTempl func(errMsg string) http.Handler
	// Observer receives auth-op observations: the session-recheck degrade and,
	// since Phase 2, login outcomes (OpBcryptLogin — rate-limited, invalid
	// credentials, or OK). Nil defaults to NopObserver — wiring an Observer is
	// purely additive and never changes auth behavior. Pass
	// identity/promobs.Observer.AsAuthObserver() to share one Prometheus
	// observer with the identity package's seam.
	Observer Observer
	// RevocationFailClosed controls liveSession's behavior when the
	// AccountStore.GetByID revocation recheck fails with a transient
	// (non-ErrAccountNotFound) error. Default false preserves today's
	// behavior: the crypto-valid session is still honored (fail open), so a
	// revoked/role-dropped operator keeps access until SessionTTL during a DB
	// outage. Set true to instead deny the request on that transient error
	// (fail closed) — trading availability for immediate revocation under a
	// degraded store. Either way the degrade is always observed via Observer.
	RevocationFailClosed bool
	// RateLimiter throttles LoginHandler's POST branch. Nil (default) means
	// no throttling — behavior is byte-for-byte identical to before Phase 2.
	// When set, LoginRate must also be set (non-zero Limit and Window) or
	// NewBcryptTOTPAuth panics at setup — a configured-but-toothless limiter
	// is a fail-closed misconfiguration, not a silent no-op. Checked
	// FAIL-CLOSED: both an over-quota deny and a limiter error (e.g. a Redis
	// outage) reject the attempt with 429 before the bcrypt compare runs, so
	// this money-path admin login fails at least as safe as identity's
	// magic-link (identity/handlers.go's allowStart convention). Pass an
	// existing identity Redis limiter directly — RateLimiter's signature
	// matches identity.RateLimiter verbatim (see ratelimit.go), so no adapter
	// or second limiter implementation is needed.
	RateLimiter RateLimiter
	// LoginRate is the (limit, window) rule applied when RateLimiter is set.
	// Ignored (and may be left zero) when RateLimiter is nil.
	LoginRate RateRule
	// ClientIP extracts the client IP used to key the login rate limit. It
	// defaults to r.RemoteAddr's host. DEPLOYERS BEHIND A REVERSE PROXY MUST
	// override this with a trusted-hop X-Forwarded-For parser — otherwise
	// every request carries the proxy's IP and the per-IP limit collapses
	// into one shared bucket (ineffective throttle / accidental site-wide
	// DoS). Mirrors identity.Config.ClientIP. Ignored when RateLimiter is nil.
	ClientIP func(*http.Request) string
}

// NewBcryptTOTPAuth validates cfg and returns a BcryptTOTPAuth. Panics on a nil
// Store, a short/empty HMACKey, or a RateLimiter set without a usable LoginRate
// (fail-closed configuration).
func NewBcryptTOTPAuth(cfg BcryptConfig) *BcryptTOTPAuth {
	if cfg.Store == nil {
		panic("auth.NewBcryptTOTPAuth: Store must not be nil")
	}
	if len(cfg.HMACKey) < minHMACKeyLen {
		panic(fmt.Sprintf("auth.NewBcryptTOTPAuth: HMACKey must be at least %d bytes, got %d", minHMACKeyLen, len(cfg.HMACKey)))
	}
	if cfg.RateLimiter != nil && (cfg.LoginRate.Limit <= 0 || cfg.LoginRate.Window <= 0) {
		panic("auth.NewBcryptTOTPAuth: LoginRate must be set (Limit > 0, Window > 0) when RateLimiter is configured")
	}
	name := cfg.CookieName
	if name == "" {
		name = defaultSessionCookie
	}
	ttl := cfg.SessionTTL
	if ttl == 0 {
		ttl = defaultSessionTTL
	}
	bp := cfg.BasePath
	if bp == "" {
		bp = "/admin"
	}
	obs := cfg.Observer
	if obs == nil {
		obs = NopObserver{}
	}
	clientIPFn := cfg.ClientIP
	if clientIPFn == nil {
		clientIPFn = defaultClientIP
	}
	return &BcryptTOTPAuth{
		store:                cfg.Store,
		hmacKey:              cfg.HMACKey,
		basePath:             bp,
		cookieName:           name,
		sessionTTL:           ttl,
		secure:               cfg.Secure,
		loginTempl:           cfg.LoginTempl,
		observer:             obs,
		revocationFailClosed: cfg.RevocationFailClosed,
		rateLimiter:          cfg.RateLimiter,
		loginRate:            cfg.LoginRate,
		clientIP:             clientIPFn,
	}
}

var _ Authenticator = (*BcryptTOTPAuth)(nil)

// RoleOwner is the super-role that RequireRole always permits.
const RoleOwner = "owner"

// rlLoginIPPrefix namespaces the login rate-limit key so it can't collide
// with another RateLimiter consumer sharing the same backing store (e.g.
// identity's magic_start:ip: keys on the same Redis instance).
const rlLoginIPPrefix = "bcrypt_login:ip:"

// defaultClientIP extracts the request's remote IP by stripping the port from
// r.RemoteAddr. Mirrors identity's default resolver (identity/handlers.go's
// clientIP) — see BcryptConfig.ClientIP's doc for the reverse-proxy caveat.
func defaultClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// dummyPasswordHash equalizes login timing: on the unknown/inactive-email path
// we compare against it so a non-existent account costs the same bcrypt work as
// a wrong password, closing the user-enumeration timing oracle. Computed once.
var dummyPasswordHash = mustDummyHash()

func mustDummyHash() string {
	h, err := HashPassword("timing-equalizer-not-a-real-password")
	if err != nil {
		panic("auth: failed to compute dummy password hash: " + err.Error())
	}
	return h
}

// SessionCookieName implements resource.sessionCookier for CSRF double-submit binding.
func (a *BcryptTOTPAuth) SessionCookieName() string { return a.cookieName }

// sessionData is the JSON payload of a bcrypt session token.
type sessionData struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	Exp    int64  `json:"exp"`
	Nonce  string `json:"n"`
}

// Session is the authenticated session exposed to downstream handlers via SessionFrom.
type Session struct {
	UserID string
	Role   string
}

type sessionCtxKey struct{}

// SessionFrom returns the Session stored on the context by Require, if present.
func SessionFrom(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionCtxKey{}).(*Session)
	return s, ok
}

// makeToken builds a signed session token: base64url(json) + "." + HMAC-SHA256.
// A per-login random nonce rotates the cookie value (and thus the CSRF binding).
func (a *BcryptTOTPAuth) makeToken(userID, role string) (string, error) {
	var nb [16]byte
	if _, err := rand.Read(nb[:]); err != nil {
		return "", fmt.Errorf("auth: crypto/rand failed: %w", err)
	}
	sd := sessionData{
		UserID: userID,
		Role:   role,
		Exp:    time.Now().Add(a.sessionTTL).Unix(),
		Nonce:  hex.EncodeToString(nb[:]),
	}
	payload, err := json.Marshal(sd)
	if err != nil {
		return "", fmt.Errorf("auth: marshal session: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.hmacKey)
	_, _ = mac.Write([]byte(encoded))
	sig := hex.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

// parseToken verifies the token MAC and expiry, returning the decoded session.
func (a *BcryptTOTPAuth) parseToken(value string) (*sessionData, bool) {
	dot := strings.LastIndex(value, ".")
	if dot < 0 {
		return nil, false
	}
	encoded, sig := value[:dot], value[dot+1:]
	if encoded == "" || sig == "" {
		return nil, false
	}
	mac := hmac.New(sha256.New, a.hmacKey)
	_, _ = mac.Write([]byte(encoded))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	var sd sessionData
	if err := json.Unmarshal(payload, &sd); err != nil {
		return nil, false
	}
	if sd.Exp == 0 || time.Now().Unix() > sd.Exp {
		return nil, false
	}
	return &sd, true
}

// Verified implements Authenticator: a fast crypto-only session check (no DB).
func (a *BcryptTOTPAuth) Verified(r *http.Request) bool {
	c, err := r.Cookie(a.cookieName)
	if err != nil {
		return false
	}
	_, ok := a.parseToken(c.Value)
	return ok
}

func (a *BcryptTOTPAuth) renderLogin(ctx context.Context, w http.ResponseWriter, errMsg string) {
	if a.loginTempl != nil {
		a.loginTempl(errMsg).ServeHTTP(w, nil) //nolint:staticcheck // nil r is intentional for template-only render
		return
	}
	ident := shell.LoginIdentifier{Label: "Email", Name: "email", Type: "email", Autocomplete: "email"}
	if err := shell.LoginPage(a.basePath, ident, errMsg).Render(ctx, w); err != nil {
		slog.Error("auth: failed to render login page", "err", err)
	}
}

// LoginHandler implements Authenticator: GET renders the form, POST runs the
// login pipeline as named steps — checkRateLimit, verifyPassword, issueSession
// — each a single-purpose, independently testable unit. A future MFA phase
// slots dispatchMFA between verifyPassword and issueSession (not implemented
// here); the seam is this ordering.
func (a *BcryptTOTPAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != http.MethodPost {
			if a.Verified(r) {
				http.Redirect(w, r, a.basePath+"/", http.StatusSeeOther)
				return
			}
			a.renderLogin(r.Context(), w, "")
			return
		}
		const maxLoginBodyBytes = 4096
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)
		_ = r.ParseForm()

		if !a.checkRateLimit(w, r) {
			return
		}

		acct, ok := a.verifyPassword(w, r, r.FormValue("email"), r.FormValue("password"))
		if !ok {
			return
		}

		// P5 (MFA login step) inserts dispatchMFA(w, r, acct) here: for
		// acct.TOTPEnabled it mints a structurally-distinct mfa_pending
		// interstitial instead of issuing a full session. Not implemented yet
		// — every account issues a session directly, unchanged from before P2.

		a.issueSession(w, r, acct)
	})
}

// checkRateLimit enforces BcryptConfig.RateLimiter/LoginRate against the
// request's client IP. Nil-safe: with no RateLimiter configured (the
// default) it always returns true and imposes no throttle — the
// pre-Phase-2 behavior is unchanged byte-for-byte. FAIL-CLOSED when a
// limiter IS configured: both an over-quota deny (Allow returns false) and a
// limiter error (e.g. a Redis outage) reject the attempt with 429 +
// Retry-After before the bcrypt compare ever runs, matching identity's
// tested convention (identity/handlers.go's allowStart). On denial this
// writes the full response and observes the terminal outcome; the caller
// must stop processing the request when it returns false.
func (a *BcryptTOTPAuth) checkRateLimit(w http.ResponseWriter, r *http.Request) bool {
	if a.rateLimiter == nil {
		return true
	}
	start := time.Now()
	key := rlLoginIPPrefix + a.clientIP(r)
	allowed, err := a.rateLimiter.Allow(r.Context(), key, a.loginRate.Limit, a.loginRate.Window)
	if err != nil {
		slog.Error("auth: login rate-limit error — denying (fail-closed)", "err", err)
		a.observer.Observe(OpBcryptLogin, OutcomeLimiterError, time.Since(start))
		a.rejectThrottled(w, r)
		return false
	}
	if !allowed {
		a.observer.Observe(OpBcryptLogin, OutcomeRateLimited, time.Since(start))
		a.rejectThrottled(w, r)
		return false
	}
	return true
}

// rejectThrottled writes the 429 response shared by both checkRateLimit
// denial branches: a Retry-After hint sized to the configured window
// (floored at 1 second, so a sub-second Window never renders "0" — a
// nonsensical "retry immediately" hint), and the login form re-rendered
// with a throttle message.
func (a *BcryptTOTPAuth) rejectThrottled(w http.ResponseWriter, r *http.Request) {
	retryAfter := max(1, int(a.loginRate.Window.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.WriteHeader(http.StatusTooManyRequests)
	a.renderLogin(r.Context(), w, "Too many login attempts. Please try again later.")
}

// verifyPassword checks email+password against the store. Preserves the
// exact pre-Phase-2 anti-enumeration timing equalization: an unknown or
// inactive email costs the same bcrypt work as a wrong password
// (dummyPasswordHash), so a timing side-channel can't distinguish the two
// failure classes. On failure it writes the 401 response, observes
// OutcomeInvalidCredentials, and returns (nil, false) — the caller must stop
// processing the request. On success it returns the account without writing
// a response, leaving session issuance (or, in a later phase, MFA dispatch)
// to the caller.
func (a *BcryptTOTPAuth) verifyPassword(w http.ResponseWriter, r *http.Request, email, password string) (*Account, bool) {
	start := time.Now()
	acct, err := a.store.GetByEmail(r.Context(), email)
	if err != nil {
		// Equalize timing with the verify path: an unknown/inactive email must
		// cost the same bcrypt work as a wrong password (no enumeration oracle).
		_ = VerifyPassword(password, dummyPasswordHash)
		a.observer.Observe(OpBcryptLogin, OutcomeInvalidCredentials, time.Since(start))
		w.WriteHeader(http.StatusUnauthorized)
		a.renderLogin(r.Context(), w, "Invalid email or password")
		return nil, false
	}
	if !VerifyPassword(password, acct.PasswordHash) {
		a.observer.Observe(OpBcryptLogin, OutcomeInvalidCredentials, time.Since(start))
		w.WriteHeader(http.StatusUnauthorized)
		a.renderLogin(r.Context(), w, "Invalid email or password")
		return nil, false
	}
	return acct, true
}

// issueSession mints the session token, sets the session cookie, best-effort
// records UpdateLastLogin, observes the terminal outcome, and redirects to
// the admin root. Terminal step of the login pipeline for every account
// today (the P5 MFA step will instead route TOTPEnabled accounts through
// dispatchMFA before ever reaching here).
func (a *BcryptTOTPAuth) issueSession(w http.ResponseWriter, r *http.Request, acct *Account) {
	start := time.Now()
	tok, err := a.makeToken(acct.ID, acct.Role)
	if err != nil {
		slog.Error("auth: failed to generate session token", "err", err)
		a.observer.Observe(OpBcryptLogin, OutcomeError, time.Since(start))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    tok,
		Path:     a.basePath,
		MaxAge:   int(a.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	if err := a.store.UpdateLastLogin(r.Context(), acct.ID); err != nil {
		slog.Warn("auth: update last login failed", "err", err)
	}
	a.observer.Observe(OpBcryptLogin, OutcomeOK, time.Since(start))
	http.Redirect(w, r, a.basePath+"/", http.StatusSeeOther)
}

// LogoutHandler implements Authenticator.
func (a *BcryptTOTPAuth) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: a.cookieName, Path: a.basePath, MaxAge: -1})
		http.Redirect(w, r, a.basePath+"/login", http.StatusSeeOther)
	})
}

// Require implements Authenticator: validates the session token AND re-checks the
// account against the store, so a deactivated / deleted / role-changed account
// loses access on the next request (instant revocation). Behavior on a
// transient store error during that recheck is controlled by
// BcryptConfig.RevocationFailClosed — see liveSession. Fail-closed on
// not-found / inactive / role drift regardless.
func (a *BcryptTOTPAuth) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sd := a.liveSession(r)
		if sd == nil {
			a.reject(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), sessionCtxKey{}, &Session{UserID: sd.UserID, Role: sd.Role})
		next(w, r.WithContext(ctx))
	}
}

// liveSession returns the validated + revocation-checked session, or nil.
//
// On a transient (non-ErrAccountNotFound) AccountStore.GetByID error, the
// degrade is always reported via a.observer (OpSessionRecheck,
// OutcomeError) so the outage is observable regardless of policy. The
// response to that error is then governed by a.revocationFailClosed:
// false (default, non-breaking) keeps the crypto-valid session live for up
// to SessionTTL; true rejects the request immediately, trading availability
// for instant revocation under a degraded store.
func (a *BcryptTOTPAuth) liveSession(r *http.Request) *sessionData {
	c, err := r.Cookie(a.cookieName)
	if err != nil {
		return nil
	}
	sd, ok := a.parseToken(c.Value)
	if !ok {
		return nil
	}
	start := time.Now()
	acct, err := a.store.GetByID(r.Context(), sd.UserID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil // account deleted -> revoke
		}
		a.observer.Observe(OpSessionRecheck, OutcomeError, time.Since(start))
		if a.revocationFailClosed {
			slog.Warn("auth: session recheck DB error — denying (RevocationFailClosed)", "err", err)
			return nil // fail closed on transient DB error
		}
		slog.Warn("auth: session recheck DB error — allowing crypto-valid token", "err", err)
		return sd // fail open on transient DB error (default)
	}
	if !acct.Active || acct.Role != sd.Role {
		return nil // deactivated or role changed -> revoke
	}
	return sd
}

func (a *BcryptTOTPAuth) reject(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", a.basePath+"/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, a.basePath+"/login", http.StatusSeeOther)
}

// RequireRole wraps Require and additionally requires the session role to equal
// role (or "owner", which is always permitted). Not part of Authenticator.
//
// On a role-denied 403, a structured warning is emitted via slog. A spike in
// these log lines indicates either a misconfigured role or a probing attempt
// (direct-URL access to a role-gated resource with insufficient privileges).
func (a *BcryptTOTPAuth) RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return a.Require(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFrom(r.Context())
		if !ok || (s.Role != role && s.Role != RoleOwner) {
			userID := ""
			actualRole := ""
			if ok {
				userID = s.UserID
				actualRole = s.Role
			}
			slog.WarnContext(r.Context(), "auth: role-denied",
				"required_role", role,
				"actual_role", actualRole,
				"user_id", userID,
				"path", r.URL.Path,
				"method", r.Method,
			)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// HasRole reports whether the session on ctx satisfies role: an exact role match
// or the "owner" super-role. It is the read-only derivation behind nav-hiding and
// must never be used as a route gate — RequireRole is the authority. Returns
// false (never panics) when ctx carries no session.
func (a *BcryptTOTPAuth) HasRole(ctx context.Context, role string) bool {
	s, ok := SessionFrom(ctx)
	return ok && (s.Role == role || s.Role == RoleOwner)
}
