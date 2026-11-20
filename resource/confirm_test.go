package resource_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/resource"
)

var confirmKey = []byte("0123456789abcdef0123456789abcdef")

// confirmPanel mounts one confirm page and the action it posts to, and returns
// a GET/POST helper that is already logged in.
func confirmPanel(t *testing.T, load func(context.Context, *http.Request) (resource.ConfirmPage, error)) (do func(method, target string, form url.Values, loggedIn bool) *httptest.ResponseRecorder) {
	t.Helper()
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw", HMACKey: confirmKey, BasePath: "/admin"})
	p := resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a, CSRFKey: confirmKey})
	p.MountConfirm(resource.ConfirmSpec{Path: "settings/revert", Load: load})
	p.MountAction(resource.ActionSpec{Path: "settings/revert", Handler: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}})
	h := p.Handler()
	lreq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login", strings.NewReader("username=op&password=pw"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lrec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(lrec, lreq)
	cookies := lrec.Result().Cookies()
	return func(method, target string, form url.Values, loggedIn bool) *httptest.ResponseRecorder {
		var req *http.Request
		if form != nil {
			req = httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequestWithContext(context.Background(), method, target, nil)
		}
		if loggedIn {
			for _, c := range cookies {
				req.AddCookie(c)
			}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
}

func goodPage(context.Context, *http.Request) (resource.ConfirmPage, error) {
	return resource.ConfirmPage{
		Title: "Revert alpha_share", Message: "A confirmation card is sent.",
		Diff:   components.Diff{Rows: []components.DiffRow{{Label: "alpha_share", From: "25 %", To: "20 %"}}},
		Action: "/admin/settings/revert", Fields: []components.HiddenField{{Name: "key", Value: "alpha_share"}},
	}, nil
}

// The page renders the diff, and its form posts a token the action route accepts.
//
// RED-on-revert: pass Token: "" in MountConfirm (resource/confirm.go) -> the
// page renders no _csrf field and the follow-up POST is 403.
func TestMountConfirm_PageRendersAndItsTokenWorks(t *testing.T) {
	do := confirmPanel(t, goodPage)
	rec := do(http.MethodGet, "/admin/settings/revert/", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`<h2>Revert alpha_share</h2>`, `<del class="diff-from">25 %</del>`, `<ins class="diff-to">20 %</ins>`,
		`action="/admin/settings/revert"`, `name="key" value="alpha_share"`, `>Confirm</button>`, `>Cancel</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	m := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no _csrf field on the confirm form")
	}
	if got := do(http.MethodPost, "/admin/settings/revert", url.Values{"_csrf": {m[1]}, "key": {"alpha_share"}}, true).Code; got != http.StatusNoContent {
		t.Errorf("POST with the page's token: %d, want 204", got)
	}
	if got := do(http.MethodPost, "/admin/settings/revert", url.Values{"key": {"alpha_share"}}, true).Code; got != http.StatusForbidden {
		t.Errorf("POST without a token: %d, want 403", got)
	}
}

// Behind the panel guard like any page.
func TestMountConfirm_RequiresLogin(t *testing.T) {
	do := confirmPanel(t, goodPage)
	if rec := do(http.MethodGet, "/admin/settings/revert/", nil, false); rec.Code == http.StatusOK {
		t.Fatalf("anonymous GET served the confirm page")
	}
}

func TestMountConfirm_LoadErrors(t *testing.T) {
	notFound := confirmPanel(t, func(context.Context, *http.Request) (resource.ConfirmPage, error) {
		return resource.ConfirmPage{}, resource.ErrConfirmNotFound
	})
	if got := notFound(http.MethodGet, "/admin/settings/revert/", nil, true).Code; got != http.StatusNotFound {
		t.Errorf("ErrConfirmNotFound: %d, want 404", got)
	}
	boom := confirmPanel(t, func(context.Context, *http.Request) (resource.ConfirmPage, error) {
		return resource.ConfirmPage{}, errors.New("db password=hunter2")
	})
	rec := boom(http.MethodGet, "/admin/settings/revert/", nil, true)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("load error: %d %q, want a generic 500", rec.Code, rec.Body.String())
	}
}

// Action and cancel are same-origin paths: a form that posts the CSRF token
// elsewhere is refused before it renders.
//
// RED-on-revert: make samePath return true (resource/confirm.go).
func TestMountConfirm_RejectsOffOriginTargets(t *testing.T) {
	for name, mutate := range map[string]func(*resource.ConfirmPage){
		"action host":      func(p *resource.ConfirmPage) { p.Action = "https://evil.example/x" },
		"action protorel":  func(p *resource.ConfirmPage) { p.Action = "//evil.example/x" },
		"action backslash": func(p *resource.ConfirmPage) { p.Action = `/\evil.example` },
		"cancel host":      func(p *resource.ConfirmPage) { p.CancelHref = "https://evil.example/" },
	} {
		t.Run(name, func(t *testing.T) {
			do := confirmPanel(t, func(ctx context.Context, r *http.Request) (resource.ConfirmPage, error) {
				p, _ := goodPage(ctx, r)
				mutate(&p)
				return p, nil
			})
			rec := do(http.MethodGet, "/admin/settings/revert/", nil, true)
			if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "evil.example") {
				t.Errorf("status %d, leaked=%v: off-origin target must be refused (500, not rendered)", rec.Code, strings.Contains(rec.Body.String(), "evil.example"))
			}
		})
	}
}

func TestMountConfirm_PanicsWithoutLoad(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MountConfirm must panic on a nil Load")
		}
	}()
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw", HMACKey: confirmKey, BasePath: "/admin"})
	resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a, CSRFKey: confirmKey}).MountConfirm(resource.ConfirmSpec{Path: "x"})
}
