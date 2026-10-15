package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// TestPanelCSRFToken_RoundTripThroughCustomPage drives the real flow a consumer
// uses: a MountPage handler asks the panel for a token, a form on that page
// posts it to a MountAction route. The token must pass; the same POST without
// it must 403 before the handler runs.
//
// Falsification (b): delete the p.verifyCSRFToken call in csrfProtect
// (resource/action.go) -> the "without token" subtest goes RED (200, ran).
// Falsification (c): make Panel.CSRFToken return "" -> the "with token"
// subtest goes RED (403).
func TestPanelCSRFToken_RoundTripThroughCustomPage(t *testing.T) {
	p := newWriterPanel()
	ran := 0
	p.MountPage(resource.PageSpec{
		Path: "board",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(p.CSRFToken(r)))
		},
	})
	p.MountAction(resource.ActionSpec{
		Path:    "board/act",
		Handler: func(w http.ResponseWriter, _ *http.Request) { ran++; w.WriteHeader(http.StatusNoContent) },
	})
	cookieVal, _ := loginAndGetCookie(t, p)
	h := p.Handler()

	get := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/board/", nil)
	get.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, get)
	tok := gw.Body.String()
	if gw.Code != http.StatusOK || tok == "" {
		t.Fatalf("page: status %d, token %q", gw.Code, tok)
	}

	t.Run("with token", func(t *testing.T) {
		ran = 0
		w := httptest.NewRecorder()
		h.ServeHTTP(w, actionPostRequest("/admin/board/act", cookieVal, url.Values{"_csrf": {tok}}))
		if w.Code != http.StatusNoContent || ran != 1 {
			t.Fatalf("status %d ran=%d, want 204/1 body=%s", w.Code, ran, w.Body.String())
		}
	})
	t.Run("without token", func(t *testing.T) {
		ran = 0
		w := httptest.NewRecorder()
		h.ServeHTTP(w, actionPostRequest("/admin/board/act", cookieVal, url.Values{}))
		if w.Code != http.StatusForbidden || ran != 0 {
			t.Fatalf("status %d ran=%d, want 403/0", w.Code, ran)
		}
	})
	t.Run("token of another session", func(t *testing.T) {
		ran = 0
		other, _ := loginAndGetCookie(t, p)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, actionPostRequest("/admin/board/act", other, url.Values{"_csrf": {tok}}))
		if w.Code != http.StatusForbidden || ran != 0 {
			t.Fatalf("status %d ran=%d, want 403/0 (token must be session-bound)", w.Code, ran)
		}
	})
}

// A panel with no CSRFKey must hand out "" (dead button) rather than a token
// signed with an empty key (a forgeable one). Falsification: drop the
// len(p.csrfKey) guard in Panel.CSRFToken -> RED (non-empty token).
func TestPanelCSRFToken_NoKeyFailsClosed(t *testing.T) {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin", Password: "secret",
		HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/admin",
	})
	p := resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: "v"})
	if tok := p.CSRFToken(req); tok != "" {
		t.Fatalf("keyless panel issued token %q", tok)
	}
}
