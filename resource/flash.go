package resource

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/anatolykoptev/go-panel/components"
)

// Flash notices: a one-shot message that survives exactly one redirect.
//
// A write handler ends in a redirect, and the page it lands on is a different
// request, so "Saved" or "Queued for confirmation" cannot be rendered by the
// handler that knows it. [AddFlash] (from a Writer hook, where only the context
// is at hand) and [Panel.Flash] (from a handler holding the request) hand the
// notice to a signed, HttpOnly cookie scoped to the panel base path; the next
// full-page render shows it above the content and clears the cookie, so a
// reload does not repeat it.
//
// The cookie carries the notice text, so it is authenticated: HMAC-SHA256 under
// a key derived from Config.CSRFKey with its own label (a CSRF token can never
// be replayed as a flash cookie or the reverse), a two-minute expiry, a cap on
// item count and size. A cookie that fails any check renders nothing and is
// cleared. A panel without a usable CSRFKey has no key to sign with and shows
// no flash at all (fail-closed, same as the write routes).
const (
	flashCookie    = "pn-flash"
	flashTTL       = 2 * time.Minute
	flashMaxItems  = 4
	flashMaxRunes  = 400  // per text field
	flashMaxCookie = 3600 // bytes of cookie value, well under the 4 KB browser limit
	flashKeyLabel  = "go-panel/flash/v1"
)

type flashCtxKey struct{}

// flashItem is the wire form of one notice. Short keys keep the cookie small.
type flashItem struct {
	Level int    `json:"l,omitempty"`
	Title string `json:"t,omitempty"`
	Text  string `json:"x"`
	Href  string `json:"h,omitempty"`
	Link  string `json:"k,omitempty"`
}

type flashPayload struct {
	Exp   int64       `json:"e"`
	Items []flashItem `json:"i"`
}

// flashState is the per-request flash bookkeeping installed by withFlash.
type flashState struct {
	mu        sync.Mutex
	incoming  []components.Notice // valid notices from the request cookie
	hasCookie bool                // the request carried a flash cookie at all
	added     []components.Notice // queued by AddFlash during this request
	consumed  bool                // a full page rendered the incoming notices
}

// AddFlash queues a notice to show on the next full-page render. It is safe to
// call from anywhere that holds the request context, including Writer.Save and
// Writer.AfterSave. A notice with no Text is ignored. Outside a panel request
// (no flash state on ctx) it logs and does nothing.
func AddFlash(ctx context.Context, n components.Notice) {
	if n.Text == "" {
		return
	}
	st, _ := ctx.Value(flashCtxKey{}).(*flashState)
	if st == nil {
		slog.Warn("resource: AddFlash called outside a panel request with a CSRFKey; notice dropped")
		return
	}
	st.mu.Lock()
	st.added = append(st.added, n)
	st.mu.Unlock()
}

// Flash queues a notice for the next full-page render, from a handler that
// holds the request (a MountAction or MountPage handler). Call it before
// writing the response. It is [AddFlash] plus a direct fallback: a request that
// did not pass through the panel's own mux writes the cookie immediately.
func (p *Panel) Flash(w http.ResponseWriter, r *http.Request, n components.Notice) {
	if n.Text == "" {
		return
	}
	if st, _ := r.Context().Value(flashCtxKey{}).(*flashState); st != nil {
		AddFlash(r.Context(), n)
		return
	}
	if len(p.csrfKey) < minCSRFKeyLen {
		slog.Warn("resource: Flash needs a usable Config.CSRFKey; notice dropped")
		return
	}
	var pending []components.Notice
	if c, err := r.Cookie(flashCookie); err == nil {
		pending = p.decodeFlash(c.Value)
	}
	p.setFlashCookie(w, r, append(pending, n))
}

// consumeFlash returns the notices the request carried and marks them shown, so
// the response clears the cookie. Nil when there are none, or when the request
// is not inside a panel with a flash key (it cannot clear what it would show,
// so it must not show it).
func consumeFlash(r *http.Request) []components.Notice {
	st, _ := r.Context().Value(flashCtxKey{}).(*flashState)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.incoming) == 0 {
		return nil
	}
	st.consumed = true
	return st.incoming
}

// withFlash installs the per-request flash state and a writer that turns it
// into a Set-Cookie header just before the response starts. A panel without a
// usable CSRFKey is passed through untouched.
func (p *Panel) withFlash(next http.Handler) http.Handler {
	if len(p.csrfKey) < minCSRFKeyLen {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &flashState{}
		if c, err := r.Cookie(flashCookie); err == nil {
			st.hasCookie = true
			st.incoming = p.decodeFlash(c.Value)
		}
		fw := &flashWriter{ResponseWriter: w, p: p, r: r, st: st}
		next.ServeHTTP(fw, r.WithContext(context.WithValue(r.Context(), flashCtxKey{}, st)))
		fw.commit()
	})
}

// flashWriter emits the flash Set-Cookie header once, before the first byte of
// the response, so notices queued late in a handler still ride the redirect.
type flashWriter struct {
	http.ResponseWriter
	p    *Panel
	r    *http.Request
	st   *flashState
	done bool
}

func (f *flashWriter) WriteHeader(code int) {
	f.commit()
	f.ResponseWriter.WriteHeader(code)
}

func (f *flashWriter) Write(b []byte) (int, error) {
	f.commit()
	return f.ResponseWriter.Write(b)
}

func (f *flashWriter) Flush() {
	f.commit()
	if fl, ok := f.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (f *flashWriter) Unwrap() http.ResponseWriter { return f.ResponseWriter }

func (f *flashWriter) commit() {
	if f.done {
		return
	}
	f.done = true
	st := f.st
	st.mu.Lock()
	defer st.mu.Unlock()
	switch {
	case len(st.added) > 0:
		// Unshown incoming notices survive alongside the new ones.
		var out []components.Notice
		if !st.consumed {
			out = append(out, st.incoming...)
		}
		f.p.setFlashCookie(f.ResponseWriter, f.r, append(out, st.added...))
	case st.hasCookie && (st.consumed || len(st.incoming) == 0):
		// Shown, or invalid/expired: either way it must not linger.
		f.p.clearFlashCookie(f.ResponseWriter, f.r)
	}
}

func (p *Panel) flashCookiePath() string {
	if p.basePath == "" {
		return "/"
	}
	return p.basePath
}

func flashSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (p *Panel) setFlashCookie(w http.ResponseWriter, r *http.Request, ns []components.Notice) {
	value, ok := p.encodeFlash(ns, time.Now().Add(flashTTL))
	if !ok {
		slog.Warn("resource: flash notice dropped: nothing fits in a cookie")
		return
	}
	//nolint:gosec // Secure follows the request scheme (TLS or X-Forwarded-Proto); HttpOnly and SameSite are set.
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: value, Path: p.flashCookiePath(),
		MaxAge: int(flashTTL / time.Second), HttpOnly: true, Secure: flashSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (p *Panel) clearFlashCookie(w http.ResponseWriter, r *http.Request) {
	//nolint:gosec // Secure follows the request scheme (TLS or X-Forwarded-Proto); HttpOnly and SameSite are set.
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: "", Path: p.flashCookiePath(),
		MaxAge: -1, HttpOnly: true, Secure: flashSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (p *Panel) flashMAC(payload []byte) []byte {
	k := hmac.New(sha256.New, p.csrfKey)
	k.Write([]byte(flashKeyLabel))
	m := hmac.New(sha256.New, k.Sum(nil))
	m.Write(payload)
	return m.Sum(nil)
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func toFlashItem(n components.Notice, maxRunes int) flashItem {
	return flashItem{
		Level: int(n.Level),
		Title: clipRunes(n.Title, maxRunes),
		Text:  clipRunes(n.Text, maxRunes),
		Href:  clipRunes(n.LinkHref, maxRunes),
		Link:  clipRunes(n.LinkText, maxRunes),
	}
}

// encodeFlash signs the newest flashMaxItems notices and shrinks them until the
// value fits flashMaxCookie: oldest notices go first, then text is clipped. It
// reports false when even one clipped notice does not fit.
func (p *Panel) encodeFlash(ns []components.Notice, exp time.Time) (string, bool) {
	if len(ns) > flashMaxItems {
		ns = ns[len(ns)-flashMaxItems:]
	}
	for _, maxRunes := range []int{flashMaxRunes, 120, 40} {
		items := make([]flashItem, len(ns))
		for i, n := range ns {
			items[i] = toFlashItem(n, maxRunes)
		}
		for len(items) > 0 {
			b, err := json.Marshal(flashPayload{Exp: exp.Unix(), Items: items})
			if err != nil {
				return "", false
			}
			v := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(p.flashMAC(b))
			if len(v) <= flashMaxCookie {
				return v, true
			}
			items = items[1:]
		}
	}
	return "", false
}

// decodeFlash verifies a cookie value and returns its notices; nil on any
// failure (bad shape, bad MAC, expired, too many items).
func (p *Panel) decodeFlash(v string) []components.Notice {
	if len(v) > flashMaxCookie+64 {
		return nil
	}
	body, sig, ok := strings.Cut(v, ".")
	if !ok {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil
	}
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(mac, p.flashMAC(payload)) {
		return nil
	}
	var fp flashPayload
	if json.Unmarshal(payload, &fp) != nil || time.Now().Unix() > fp.Exp || len(fp.Items) > flashMaxItems {
		return nil
	}
	out := make([]components.Notice, 0, len(fp.Items))
	for _, it := range fp.Items {
		if it.Text == "" {
			continue
		}
		lvl := components.NoticeLevel(it.Level)
		if lvl < components.NoticeInfo || lvl > components.NoticeMuted {
			lvl = components.NoticeInfo
		}
		out = append(out, components.Notice{
			Level: lvl, Title: clipRunes(it.Title, flashMaxRunes), Text: clipRunes(it.Text, flashMaxRunes),
			LinkHref: clipRunes(it.Href, flashMaxRunes), LinkText: clipRunes(it.Link, flashMaxRunes),
		})
	}
	return out
}
