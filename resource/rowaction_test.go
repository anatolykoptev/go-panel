package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
)

func rowActionResource(actions []resource.RowAction) resource.Resource {
	return resource.Resource{
		Name: "history", Title: "History", Icon: "*",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Label: "ID", SQLExpr: "id", Sortable: true}},
			DefaultKey: "id", DefaultDir: admintable.Asc,
		},
		RowActions: actions,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{
				{ID: "r1", Cells: []resource.Cell{{Value: "one"}}, Actions: []string{"revert"}},
				{ID: "r2", Cells: []resource.Cell{{Value: "two"}}},
				{ID: "r3", Cells: []resource.Cell{{Value: "three"}}, Actions: []string{"nope"}},
			}, 3, nil
		},
	}
}

func revertAction() resource.RowAction {
	return resource.RowAction{
		Key:   "revert",
		Label: "Revert",
		Path:  func(id string) string { return "history/" + url.PathEscape(id) + "/revert" },
		Confirm: func(r resource.Row) string {
			return "Revert " + r.ID + "?"
		},
		Tone: components.ToneRed,
	}
}

// rowActionPanel registers the resource and a MountAction route that records
// the ids it served.
func rowActionPanel(t *testing.T, actions []resource.RowAction) (*resource.Panel, *[]string) {
	t.Helper()
	p := newWriterPanel()
	resource.Register(p, rowActionResource(actions))
	var served []string
	p.MountAction(resource.ActionSpec{
		Path: "history/{id}/revert",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			served = append(served, r.PathValue("id"))
			w.WriteHeader(http.StatusNoContent)
		},
	})
	return p, &served
}

func rowActionGet(t *testing.T, p *resource.Panel, cookie string) string {
	t.Helper()
	return getPage(t, p, cookie, "/admin/history", false)
}

var rowActionForm = regexp.MustCompile(`(?s)<form method="post" action="([^"]*)"[^>]*>.*?</form>`)

func TestRowActions_RendersTokenedFormOnlyForOptedInRows(t *testing.T) {
	p, _ := rowActionPanel(t, []resource.RowAction{revertAction()})
	cookie := loginCookie(t)
	body := rowActionGet(t, p, cookie)

	forms := rowActionForm.FindAllStringSubmatch(body, -1)
	if len(forms) != 1 {
		t.Fatalf("rendered %d action forms, want exactly 1 (only r1 opts in)\n%s", len(forms), body)
	}
	if forms[0][1] != "/admin/history/r1/revert" {
		t.Errorf("form action = %q, want /admin/history/r1/revert", forms[0][1])
	}
	if !strings.Contains(forms[0][0], `data-confirm="Revert r1?"`) {
		t.Errorf("form lacks data-confirm prompt: %s", forms[0][0])
	}
	tok := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(forms[0][0])
	if tok == nil {
		t.Fatalf("action form carries no _csrf token: %s", forms[0][0])
	}
	if err := csrf.Verify(testCSRFKey, cookie, tok[1]); err != nil {
		t.Errorf("rendered token does not verify against the session: %v", err)
	}
	if !strings.Contains(forms[0][0], "btn-danger") {
		t.Errorf("ToneRed action should use the danger button: %s", forms[0][0])
	}
}

func TestRowActions_RenderedTokenAuthorisesTheMountedRoute(t *testing.T) {
	p, served := rowActionPanel(t, []resource.RowAction{revertAction()})
	cookie := loginCookie(t)
	body := rowActionGet(t, p, cookie)
	tok := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if tok == nil {
		t.Fatal("no token rendered")
	}
	post := func(token string) int {
		form := url.Values{}
		if token != "" {
			form.Set("_csrf", token)
		}
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/history/r1/revert",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookie})
		w := httptest.NewRecorder()
		p.Handler().ServeHTTP(w, req)
		return w.Code
	}
	if code := post(""); code != http.StatusForbidden || len(*served) != 0 {
		t.Fatalf("tokenless POST: status %d, handler ran %d times; want 403 and 0", code, len(*served))
	}
	if code := post(tok[1]); code != http.StatusNoContent || len(*served) != 1 || (*served)[0] != "r1" {
		t.Fatalf("rendered-token POST: status %d, served %v; want 204 and [r1]", code, *served)
	}
}

func TestRowActions_NoneDeclaredAddsNothing(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, rowActionResource(nil))
	body := rowActionGet(t, p, loginCookie(t))
	if strings.Contains(body, `name="_csrf"`) || strings.Contains(body, "row-actions") {
		t.Fatalf("a resource without RowActions rendered action markup:\n%s", body)
	}
}

func TestRowActions_RegisterFailsClosed(t *testing.T) {
	noCSRF := func() *resource.Panel {
		a := auth.NewHMACAuth(auth.HMACConfig{Username: "a", Password: "b",
			HMACKey: []byte("test-hmac-key-32-bytes-long-here"), BasePath: "/admin"})
		return resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a})
	}
	ok := revertAction()
	tests := []struct {
		name    string
		panel   func() *resource.Panel
		actions []resource.RowAction
	}{
		{"no CSRF key", noCSRF, []resource.RowAction{ok}},
		{"empty key", newWriterPanel, []resource.RowAction{{Label: "x", Path: ok.Path}}},
		{"duplicate key", newWriterPanel, []resource.RowAction{ok, ok}},
		{"empty label", newWriterPanel, []resource.RowAction{{Key: "k", Path: ok.Path}}},
		{"nil path", newWriterPanel, []resource.RowAction{{Key: "k", Label: "x"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register accepted an invalid RowActions declaration")
				}
			}()
			resource.Register(tc.panel(), rowActionResource(tc.actions))
		})
	}
}
