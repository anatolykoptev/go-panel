package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

const testHMACKey = "0123456789abcdef0123456789abcdef"

func loginCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login",
		strings.NewReader("username=admin&password=demo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "panel_admin" {
			return c
		}
	}
	t.Fatalf("no session cookie (status %d)", rec.Code)
	return nil
}

// TestShowcase_RendersEveryComponentWithLiveTokens is the reviewer-facing
// golden check: the page carries a marker for each new component, and the
// CSRF token on its POST controls is one the panel will actually accept.
func TestShowcase_RendersEveryComponentWithLiveTokens(t *testing.T) {
	h := buildPanel([]byte(testHMACKey), "admin", "demo").Handler()
	ck := loginCookie(t, h)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/showcase/", nil)
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, marker := range []string{
		`class="btn btn-primary"`, `class="btn btn-danger"`, `class="badge badge-orange"`, `class="badge badge-purple"`,
		`class="kpi-strip"`, `class="queue-row"`, `class="funnel"`, `class="stepper"`, `class="timeline"`,
		`class="dl-compact"`, `<select id="sc-status" class="pm7-input">`, `type="datetime-local"`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("showcase lacks %s", marker)
		}
	}
	m := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no _csrf token rendered on the page's POST controls")
	}

	post := func(tok string) int {
		f := url.Values{}
		if tok != "" {
			f.Set("_csrf", tok)
		}
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/showcase/take", strings.NewReader(f.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("HX-Request", "true")
		r.AddCookie(ck)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := post(m[1]); got != http.StatusOK {
		t.Errorf("POST with the page's own token: %d, want 200", got)
	}
	if got := post(""); got != http.StatusForbidden {
		t.Errorf("POST without token: %d, want 403", got)
	}
}

// The demo list opts into the new list features: chip labels and an emphasised row.
func TestExampleList_UsesFilterLabelsAndEmphasis(t *testing.T) {
	h := buildPanel([]byte(testHMACKey), "admin", "demo").Handler()
	ck := loginCookie(t, h)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/places", nil)
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, ">Published</button>") || !strings.Contains(body, `<tr class="row-emphasis">`) {
		t.Errorf("example list does not show a labelled chip and an emphasised row")
	}
}
