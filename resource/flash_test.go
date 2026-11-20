package resource_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

func flashPanel(t *testing.T) (*resource.Panel, string) {
	t.Helper()
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(ctx context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			resource.AddFlash(ctx, components.Notice{Level: components.NoticeInfo, Text: "Queued <b>for</b> confirmation"})
			return nil
		},
	))
	cookie, _ := loginAndGetCookie(t, p)
	return p, cookie
}

func flashCookieFrom(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "pn-flash" {
			return c
		}
	}
	return nil
}

func listWithFlash(p *resource.Panel, session string, flash *http.Cookie, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/items", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: session})
	if flash != nil {
		req.AddCookie(flash)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

func TestFlash_ShownOnceAfterSave(t *testing.T) {
	p, session := flashPanel(t)
	save := postSaveHandlerTest(t, p, session, url.Values{"name": {"x"}})
	set := flashCookieFrom(t, save)
	if set == nil || set.MaxAge <= 0 || !set.HttpOnly {
		t.Fatalf("save did not set an HttpOnly flash cookie: %+v (status %d)", set, save.Code)
	}

	// An htmx fragment request must not consume it.
	frag := listWithFlash(p, session, set, true)
	if flashCookieFrom(t, frag) != nil {
		t.Fatal("a fragment request cleared the flash cookie before any page showed it")
	}

	page := listWithFlash(p, session, set, false)
	body := page.Body.String()
	if !strings.Contains(body, `role="status"`) || !strings.Contains(body, "Queued &lt;b&gt;for&lt;/b&gt; confirmation") {
		t.Fatalf("flash not rendered (escaped) in role=status block:\n%s", body)
	}
	cleared := flashCookieFrom(t, page)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("page render did not clear the flash cookie: %+v", cleared)
	}

	// The browser drops the cleared cookie, so the next page has none.
	next := listWithFlash(p, session, nil, false)
	if strings.Contains(next.Body.String(), "Queued") {
		t.Fatal("flash shown a second time")
	}
}

func TestFlash_TamperedCookieRendersNothingAndIsCleared(t *testing.T) {
	p, session := flashPanel(t)
	set := flashCookieFrom(t, postSaveHandlerTest(t, p, session, url.Values{"name": {"x"}}))
	if set == nil {
		t.Fatal("no flash cookie set")
	}
	// Rewrite the notice text but keep the original signature: valid JSON, wrong MAC.
	body, sig, _ := strings.Cut(set.Value, ".")
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || !strings.Contains(string(raw), "Queued") {
		t.Fatalf("unexpected cookie payload %q (%v)", raw, err)
	}
	forged := strings.Replace(string(raw), "Queued", "Hacked", 1)
	bad := &http.Cookie{Name: set.Name, Value: base64.RawURLEncoding.EncodeToString([]byte(forged)) + "." + sig}
	page := listWithFlash(p, session, bad, false)
	if strings.Contains(page.Body.String(), "Hacked") || strings.Contains(page.Body.String(), "Queued") || strings.Contains(page.Body.String(), `class="flash"`) {
		t.Fatal("a cookie with a bad MAC rendered a notice")
	}
	if c := flashCookieFrom(t, page); c == nil || c.MaxAge >= 0 {
		t.Fatalf("an invalid flash cookie was not cleared: %+v", c)
	}
}

func TestFlash_PanelFlashFromMountedAction(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	))
	p.MountAction(resource.ActionSpec{
		Path: "items/{id}/withdraw",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			p.Flash(w, r, components.Notice{Level: components.NoticeWarn, Text: "Withdrawn"})
			http.Redirect(w, r, "/admin/items", http.StatusSeeOther)
		},
	})
	session, _ := loginAndGetCookie(t, p)
	form := url.Values{}
	form.Set("_csrf", csrf.Issue(testCSRFKey, session, csrf.DefaultTTL))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/7/withdraw", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: session})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	set := flashCookieFrom(t, w)
	if w.Code != http.StatusSeeOther || set == nil {
		t.Fatalf("status %d, flash cookie %+v; want 303 and a cookie", w.Code, set)
	}
	if !strings.Contains(listWithFlash(p, session, set, false).Body.String(), "Withdrawn") {
		t.Fatal("Panel.Flash notice not shown on the next page")
	}
}
