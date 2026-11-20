package shell_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/shell"
)

// The layout used to hardcode /admin/static and /admin/logout, so a panel
// mounted anywhere else loaded no CSS or JS and logged out into a 404.
//
// RED-on-revert: put "/admin/static" back into staticURL, or "/admin/logout"
// into logoutURL (shell/layout.templ).
func TestLayout_FollowsBasePath(t *testing.T) {
	cases := []struct {
		name  string
		state shell.ChromeState
	}{
		{"base path only", shell.ChromeState{BasePath: "/ops"}},
		{"base path and static base", shell.ChromeState{BasePath: "/ops", StaticBase: "/ops/static"}},
		{"profile block", shell.ChromeState{BasePath: "/ops", Profile: shell.ProfileConfig{Name: "Olga"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := renderLayout(t, shell.ContextWithChrome(context.Background(), tc.state), nil)
			for _, want := range []string{
				`href="/ops/static/pm7.css"`,
				`src="/ops/static/htmx.min.js"`,
				`src="/ops/static/pm7.js"`,
				`src="/ops/static/admin.js"`,
				`action="/ops/logout"`,
			} {
				if !strings.Contains(html, want) {
					t.Errorf("missing %s", want)
				}
			}
			for _, bad := range []string{"/admin/static", "/admin/logout"} {
				if strings.Contains(html, bad) {
					t.Errorf("layout still links %s under a /ops mount", bad)
				}
			}
		})
	}
}

// An explicit Profile.LogoutURL still wins over the derived one.
func TestLayout_ProfileLogoutURLWins(t *testing.T) {
	st := shell.ChromeState{BasePath: "/ops", Profile: shell.ProfileConfig{Name: "Olga", LogoutURL: "/sso/out"}}
	html := renderLayout(t, shell.ContextWithChrome(context.Background(), st), nil)
	if !strings.Contains(html, `action="/sso/out"`) || strings.Contains(html, `action="/ops/logout"`) {
		t.Errorf("Profile.LogoutURL must win")
	}
}

// A zero ChromeState (a consumer rendering Layout directly) keeps /admin.
func TestLayout_ZeroStateKeepsAdminDefaults(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	for _, want := range []string{`href="/admin/static/pm7.css"`, `action="/admin/logout"`} {
		if !strings.Contains(html, want) {
			t.Errorf("zero state lost the default %s", want)
		}
	}
}

// The golden for a second base path: the diff against chrome_zero_golden.html
// is exactly the asset and logout URLs.
func TestChrome_SecondBasePath_GoldenStable(t *testing.T) {
	ctx := shell.ContextWithChrome(context.Background(), shell.ChromeState{BasePath: "/ops", StaticBase: "/ops/static"})
	got := renderLayout(t, ctx, nil)
	path := filepath.Join("testdata", "chrome_ops_golden.html")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden missing; regenerate with: go test ./shell/ -update: %v", err)
	}
	if string(want) != got {
		t.Fatalf("layout under /ops differs from %s; refresh with -update", path)
	}
}

// The phone top bar carries the page name (the layout title), and the sidebar
// title keeps the full text in a title attribute for when it is truncated.
//
// RED-on-revert: remove the mobile-bar-title span or the title attribute on the
// sidebar <h1> in shell/layout.templ.
func TestLayout_PageNameInPhoneBarAndSidebarTitleAttr(t *testing.T) {
	html := renderLayout(t, context.Background(), nil)
	if !strings.Contains(html, `<span class="mobile-bar-title" aria-hidden="true">T</span>`) {
		t.Errorf("phone top bar lacks the page name")
	}
	if !strings.Contains(html, `<h1 title="T">T</h1>`) {
		t.Errorf("sidebar title lacks its title attribute")
	}
}
