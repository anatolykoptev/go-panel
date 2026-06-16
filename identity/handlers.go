package identity

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anatolykoptev/go-panel/identity/session"
)

const (
	rlEmailPrefix = "magic_start:email:"
	rlIPPrefix    = "magic_start:ip:"
	rootPath      = "/"

	// maxBodyBytes bounds request bodies parsed by these handlers.
	maxBodyBytes = 1 << 16

	// maxEmailLen is the RFC 5321 maximum address length.
	maxEmailLen = 254
	// minPrintable is the first printable ASCII byte; anything below it (CR, LF,
	// NUL, etc.) is a control character and is rejected to prevent header
	// injection when the address is interpolated into an email header.
	minPrintable = 0x20
	delChar      = 0x7f
)

// MagicStartHandler begins the passwordless flow. It ALWAYS returns 204 — for a
// valid, malformed, or rate-limited email alike — so the response never reveals
// whether an account exists (no user enumeration). Failures are logged, never
// surfaced.
func MagicStartHandler(a *PublicAuthenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		start := time.Now()
		emailAddr, returnTo := parseStartRequest(r)

		// Every path below collapses to 204 (no enumeration).
		defer w.WriteHeader(http.StatusNoContent)

		if !validEmail(emailAddr) {
			a.cfg.Observer.Observe(OpMagicStart, OutcomeBadRequest, time.Since(start))
			return
		}
		if !a.allowStart(ctx, emailAddr, a.cfg.ClientIP(r)) {
			a.cfg.Observer.Observe(OpMagicStart, OutcomeRateLimited, time.Since(start))
			return
		}
		token, err := a.magic.Start(ctx, emailAddr)
		if err != nil {
			a.log.ErrorContext(ctx, "identity: magic start failed", slog.String("err", err.Error()))
			a.cfg.Observer.Observe(OpMagicStart, OutcomeError, time.Since(start))
			return
		}
		link := a.magicLink(token, returnTo)
		htmlBody, textBody := magicEmailBodies(link)
		if err := a.cfg.Email.Send(ctx, emailAddr, a.cfg.EmailSubject, htmlBody, textBody); err != nil {
			a.log.ErrorContext(ctx, "identity: magic email send failed", slog.String("err", err.Error()))
			a.cfg.Observer.Observe(OpMagicStart, OutcomeError, time.Since(start))
			return
		}
		a.cfg.Observer.Observe(OpMagicStart, OutcomeOK, time.Since(start))
	})
}

// MagicVerifyHandler consumes the token, upserts the identity, mints a fresh
// session (rotating any existing one), optionally links the anon device, and
// redirects to a validated return target.
func MagicVerifyHandler(a *PublicAuthenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		start := time.Now()

		id, err := a.magic.Verify(ctx, r.URL.Query().Get("token"))
		if err != nil {
			a.cfg.Observer.Observe(OpMagicVerify, OutcomeInvalidToken, time.Since(start))
			a.redirectLogin(w, r)
			return
		}

		// ADR-002: the store only ever sees HMAC(email, pepper), never the raw email.
		uidHash := a.cfg.Hasher([]byte(id.Email))
		userID, _, err := a.cfg.Users.UpsertIdentity(ctx, id.ProviderName, uidHash)
		if err != nil {
			a.log.ErrorContext(ctx, "identity: upsert identity failed", slog.String("err", err.Error()))
			a.cfg.Observer.Observe(OpMagicVerify, OutcomeError, time.Since(start))
			a.redirectLogin(w, r)
			return
		}
		snap, err := a.cfg.Users.GetUserSnapshot(ctx, userID)
		if err != nil {
			a.log.ErrorContext(ctx, "identity: snapshot failed", slog.String("err", err.Error()))
			a.cfg.Observer.Observe(OpMagicVerify, OutcomeError, time.Since(start))
			a.redirectLogin(w, r)
			return
		}

		// Session-fixation defense: always mint a NEW sid from the fresh snapshot
		// and revoke any pre-existing session bound to the old cookie.
		oldSID := cookieValue(r, a.cfg.Cookie.Name)
		newSID, err := a.cfg.Sessions.Create(ctx, a.stampSnapshot(snap), a.cfg.SessionTTL)
		if err != nil {
			a.log.ErrorContext(ctx, "identity: session create failed", slog.String("err", err.Error()))
			a.cfg.Observer.Observe(OpMagicVerify, OutcomeError, time.Since(start))
			a.redirectLogin(w, r)
			return
		}
		if oldSID != "" {
			if err := a.cfg.Sessions.Revoke(ctx, oldSID); err != nil {
				a.log.WarnContext(ctx, "identity: revoke old session failed", slog.String("err", err.Error()))
			}
		}

		if epid := cookieValue(r, a.cfg.DeviceCookieName); epid != "" {
			if err := a.cfg.Users.LinkDevice(ctx, epid, userID); err != nil {
				a.log.WarnContext(ctx, "identity: link device failed", slog.String("err", err.Error()))
			}
		}

		ck := a.cfg.Cookie.Build(r.Host, newSID)
		ck.MaxAge = int(a.cfg.SessionTTL.Seconds())
		http.SetCookie(w, ck)

		a.cfg.Observer.Observe(OpMagicVerify, OutcomeOK, time.Since(start))
		http.Redirect(w, r, safeReturnURL(r.URL.Query().Get("return"), r.Host), http.StatusFound)
	})
}

// LogoutHandler revokes the current session and expires the cookie.
//
// CSRF posture: this is a state-changing POST protected by the session cookie's
// SameSite=Lax attribute (a cross-site POST does not carry the cookie). If a
// deployment needs stronger protection it should wrap this handler with the
// repo's csrf package before mounting.
func LogoutHandler(a *PublicAuthenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		start := time.Now()
		outcome := OutcomeOK
		if sid := cookieValue(r, a.cfg.Cookie.Name); sid != "" {
			if err := a.cfg.Sessions.Revoke(r.Context(), sid); err != nil {
				a.log.WarnContext(r.Context(), "identity: logout revoke failed", slog.String("err", err.Error()))
				outcome = OutcomeError
			}
		}
		http.SetCookie(w, a.cfg.Cookie.Expire(r.Host))
		a.cfg.Observer.Observe(OpLogout, outcome, time.Since(start))
		http.Redirect(w, r, rootPath, http.StatusFound)
	})
}

// LinkDeviceHandler links an anonymous device id to the authenticated user. It
// requires a live session.
//
// CSRF posture: state-changing POST guarded by SameSite=Lax plus the
// application/json content-type requirement (which forces a CORS preflight for
// cross-origin callers). Wrap with the csrf package if stronger protection is
// required.
func LinkDeviceHandler(a *PublicAuthenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		start := time.Now()
		sid := cookieValue(r, a.cfg.Cookie.Name)
		if sid == "" {
			a.cfg.Observer.Observe(OpLinkDevice, OutcomeBadRequest, time.Since(start))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		snap, err := a.cfg.Sessions.Get(ctx, sid)
		if err != nil {
			a.cfg.Observer.Observe(OpLinkDevice, OutcomeInvalidToken, time.Since(start))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var body struct {
			Epid string `json:"epid"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&body); err != nil || body.Epid == "" {
			a.cfg.Observer.Observe(OpLinkDevice, OutcomeBadRequest, time.Since(start))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := a.cfg.Users.LinkDevice(ctx, body.Epid, snap.UserID); err != nil {
			a.log.ErrorContext(ctx, "identity: link device failed", slog.String("err", err.Error()))
			a.cfg.Observer.Observe(OpLinkDevice, OutcomeError, time.Since(start))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		a.cfg.Observer.Observe(OpLinkDevice, OutcomeOK, time.Since(start))
		w.WriteHeader(http.StatusNoContent)
	})
}

// ---- helpers ----------------------------------------------------------------

func parseStartRequest(r *http.Request) (emailAddr, returnTo string) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Email  string `json:"email"`
			Return string `json:"return"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&body)
		return strings.TrimSpace(body.Email), body.Return
	}
	return strings.TrimSpace(r.FormValue("email")), r.FormValue("return")
}

// validEmail is a deliberately minimal syntactic check: bounded length, no
// control characters, one "@", a non-empty local part, and a dotted domain.
// Deliverability is the provider's concern.
//
// Rejecting control characters (CR, LF, NUL, …) here is security-load-bearing:
// the address is interpolated into email headers downstream, and a pluggable
// EmailSender (e.g. an HTTP API backend) without its own guard would otherwise
// permit header injection (Bcc smuggling → magic-link token theft).
func validEmail(s string) bool {
	if len(s) == 0 || len(s) > maxEmailLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < minPrintable || s[i] == delChar {
			return false
		}
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	domain := s[at+1:]
	if strings.IndexByte(domain, '@') >= 0 {
		return false
	}
	return strings.Contains(domain, ".")
}

// allowStart fails closed: a limiter error denies the attempt rather than
// risking unmetered abuse.
func (a *PublicAuthenticator) allowStart(ctx context.Context, emailAddr, ip string) bool {
	okEmail, err := a.cfg.RateLimiter.Allow(ctx, rlEmailPrefix+emailAddr, a.cfg.EmailRate.Limit, a.cfg.EmailRate.Window)
	if err != nil {
		a.log.ErrorContext(ctx, "identity: email rate-limit error", slog.String("err", err.Error()))
		return false
	}
	okIP, err := a.cfg.RateLimiter.Allow(ctx, rlIPPrefix+ip, a.cfg.IPRate.Limit, a.cfg.IPRate.Window)
	if err != nil {
		a.log.ErrorContext(ctx, "identity: ip rate-limit error", slog.String("err", err.Error()))
		return false
	}
	return okEmail && okIP
}

func (a *PublicAuthenticator) magicLink(token, returnTo string) string {
	link := a.cfg.BaseURL + verifyPath + "?token=" + url.QueryEscape(token)
	if returnTo != "" {
		link += "&return=" + url.QueryEscape(returnTo)
	}
	return link
}

func magicEmailBodies(link string) (htmlBody, textBody string) {
	textBody = "Sign in to your account:\n\n" + link +
		"\n\nThis link can be used once and expires shortly. If you did not request it, ignore this email."
	htmlBody = `<p>Sign in to your account:</p>` +
		`<p><a href="` + link + `">Sign in</a></p>` +
		`<p>This link can be used once and expires shortly. If you did not request it, ignore this email.</p>`
	return htmlBody, textBody
}

func (a *PublicAuthenticator) stampSnapshot(s session.UserSnapshot) session.UserSnapshot {
	now := time.Now().UTC()
	s.IssuedAt = now
	s.ExpiresAt = now.Add(a.cfg.SessionTTL)
	return s
}

func (a *PublicAuthenticator) redirectLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, a.cfg.LoginPath, http.StatusFound)
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// safeReturnURL prevents open redirects: it accepts only same-origin relative
// paths. Absolute URLs to other hosts, protocol-relative ("//evil"), and
// backslash tricks ("/\\evil") all collapse to "/".
func safeReturnURL(raw, host string) string {
	if raw == "" {
		return rootPath
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `/\`) || strings.HasPrefix(raw, `\`) {
		return rootPath
	}
	// Defense-in-depth: reject a percent-encoded "//"/"/\" that decodes to a
	// protocol-relative target (non-exploitable in Go's http.Redirect today, but
	// cheap to guard).
	if dec, err := url.PathUnescape(raw); err == nil {
		if strings.HasPrefix(dec, "//") || strings.HasPrefix(dec, `/\`) {
			return rootPath
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return rootPath
	}
	if u.IsAbs() || u.Host != "" {
		if u.Host == host {
			return u.RequestURI()
		}
		return rootPath
	}
	if strings.HasPrefix(raw, rootPath) {
		return raw
	}
	return rootPath
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
