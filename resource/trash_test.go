package resource_test

// The panel-wide Trash.
//
// What carries weight here is not that the page renders. It is that every way
// the page can be WRONG WITHOUT AN ERROR is pinned: a resource that quietly
// leaks into an operator's trash, a lister failure that reads as "you deleted
// nothing", a cap that reads as the whole set, and a page that appears in
// panels which never asked for one. Each of those looks exactly like a working
// Trash from the outside, which is why each gets a test naming the edit that
// turns it red.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// trashRoleAuth is HMACAuth plus a controllable RoleAuthenticator, so a test can
// be an operator who holds one role and not another. Embedding the real auth
// keeps SessionCookieName — a Writer resource will not register without it.
type trashRoleAuth struct {
	*auth.HMACAuth
	allow map[string]bool
}

func (a *trashRoleAuth) RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return a.Require(func(w http.ResponseWriter, r *http.Request) {
		if !a.allow[role] {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (a *trashRoleAuth) HasRole(_ context.Context, role string) bool { return a.allow[role] }

func newTrashRoleAuth(allow map[string]bool) *trashRoleAuth {
	return &trashRoleAuth{
		HMACAuth: auth.NewHMACAuth(auth.HMACConfig{
			Username: "admin",
			Password: "secret",
			HMACKey:  []byte("test-hmac-key-32-bytes-long-here"),
			BasePath: "/admin",
			Secure:   false,
		}),
		allow: allow,
	}
}

// trashResource is a writer resource that can delete, restore, and list its
// deleted rows — the full opt-in. rows/total/err drive what its TrashLister
// reports.
func trashResource(name string, rows []resource.Row, total int, err error) resource.Resource {
	r := undoResourceBare()
	r.Name = name
	r.Title = strings.ToUpper(name[:1]) + name[1:]
	r.TrashLister = func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
		if err != nil {
			return nil, 0, err
		}
		return rows, total, nil
	}
	return r
}

// undoResourceBare is trashResource's base: Delete + Restore, no TrashLister.
func undoResourceBare() resource.Resource {
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error { return nil },
	)
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error { return nil }
	r.Writer.Restore = func(_ context.Context, _ tenant.Tenant, _ string) error { return nil }
	return r
}

func trashRows(n int) []resource.Row {
	out := make([]resource.Row, 0, n)
	for i := range n {
		id := string(rune('a' + i))
		out = append(out, resource.Row{
			ID:    id,
			Cells: []resource.Cell{{Value: "Row-" + id}, {Value: "2026-08-20"}},
		})
	}
	return out
}

// getTrashPage fetches a page and returns status + body, so a test can assert a 404
// as easily as a render.
func getTrashPage(t *testing.T, p *resource.Panel, cookieVal, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// A panel whose resources never opted in must be byte-identical to one built
// before the Trash existed: no route, no sidebar entry. Every go-panel consumer
// picks this version up on a bump without asking for a trash.
//
// Falsification: in resource/trash.go, replace mountTrash's
// `if !p.hasTrash() { return }` with `if false { return }` → RED.
func TestTrash_AbsentUntilAResourceOptsIn(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, undoResourceBare()) // Delete + Restore, but no TrashLister
	cookieVal, _ := loginAndGetCookie(t, p)

	code, _ := getTrashPage(t, p, cookieVal, "/admin/trash")
	if code != http.StatusNotFound {
		t.Errorf("no resource opted in, yet /admin/trash answered %d — every consumer would gain a page it never asked for", code)
	}
	_, list := getTrashPage(t, p, cookieVal, "/admin/items")
	if strings.Contains(list, "/admin/trash") {
		t.Error("the sidebar links to a Trash page that does not exist")
	}
}

// The opt-in mounts the page, the sidebar entry, and a way back for each row.
func TestTrash_OptInRendersRowsAndTheWayBack(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, trashResource("items", trashRows(2), 2, nil))
	cookieVal, _ := loginAndGetCookie(t, p)

	code, body := getTrashPage(t, p, cookieVal, "/admin/trash")
	if code != http.StatusOK {
		t.Fatalf("GET /admin/trash: %d", code)
	}
	if !strings.Contains(body, "Row-a") || !strings.Contains(body, "Row-b") {
		t.Error("deleted rows are missing from the trash page")
	}
	// The button must post to THIS resource's restore route: on a panel-wide
	// page the row carries the only clue about which resource owns it, so a
	// wrong URL here restores nothing (or, worse, someone else's row).
	if !strings.Contains(body, `hx-post="/admin/items/a/restore"`) {
		t.Error("the Вернуть button does not post to the row's own resource restore route")
	}
	if !strings.Contains(body, csrf.FormField) {
		t.Errorf("the restore button carries no %q field, so every click 403s — "+
			"a button that is present, looks right, and has never once run", csrf.FormField)
	}
	_, list := getTrashPage(t, p, cookieVal, "/admin/items")
	if !strings.Contains(list, "/admin/trash") {
		t.Error("a panel with a trash does not offer it in the sidebar")
	}
}

// The trash must show only what this operator could have reached anyway.
//
// Falsification: in resource/trash.go, delete the
// `if r.RequiredRole != "" && (ra == nil || !ra.HasRole(...))` block in
// trashResourcesFor → RED.
func TestTrash_OmitsAResourceTheOperatorCannotReach(t *testing.T) {
	a := newTrashRoleAuth(map[string]bool{"editor": true, "finance": false})
	p := resource.New(resource.Config{
		Title: "Test Panel", BasePath: "/admin", Auth: a, CSRFKey: testCSRFKey,
	})
	mine := trashResource("items", trashRows(1), 1, nil)
	mine.RequiredRole = "editor"
	theirs := trashResource("invoices", []resource.Row{
		{ID: "z", Cells: []resource.Cell{{Value: "SECRET-INVOICE"}, {Value: "2026-08-20"}}},
	}, 1, nil)
	theirs.RequiredRole = "finance"
	resource.Register(p, mine)
	resource.Register(p, theirs)
	cookieVal, _ := loginAndGetCookie(t, p)

	code, body := getTrashPage(t, p, cookieVal, "/admin/trash")
	if code != http.StatusOK {
		t.Fatalf("GET /admin/trash: %d", code)
	}
	if !strings.Contains(body, "Row-a") {
		t.Error("the operator's own trashed row is missing")
	}
	if strings.Contains(body, "SECRET-INVOICE") {
		t.Error("a role-gated resource's deleted rows leaked into the trash of an operator " +
			"who cannot open that resource — the trash became a read hole around RequiredRole")
	}
}

// A lister that fails must SAY so. Skipping the section renders the same page
// as "nothing was deleted here", and the row the operator came to recover is
// exactly the one they would then stop looking for.
//
// Falsification: in resource/trash.go's handleTrash, replace the
// `d.Sections = append(d.Sections, trashSection{Resource: r, Err: true})` line
// with `continue` → RED.
func TestTrash_AListerFailureIsVisible(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, trashResource("items", nil, 0, errors.New("db is down")))
	cookieVal, _ := loginAndGetCookie(t, p)

	code, body := getTrashPage(t, p, cookieVal, "/admin/trash")
	if code != http.StatusOK {
		t.Fatalf("GET /admin/trash: %d", code)
	}
	if !strings.Contains(body, "trash-unreadable") {
		t.Error("a failed lister rendered no failure marker")
	}
	if strings.Contains(body, "Trash is empty") {
		t.Error("a failed lister rendered as an EMPTY trash — the operator is told their " +
			"deleted rows are gone when the page simply could not read them")
	}
}

// Nothing is ever purged, so a resource's trash only grows past the page cap.
// A cap the operator cannot see reads as the complete set.
//
// Falsification: in resource/trash.templ, delete the
// `if s.Total > len(s.Rows)` block from trashSectionBlock → RED.
func TestTrash_AnnouncesWhatItDidNotShow(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, trashResource("items", trashRows(3), 300, nil))
	cookieVal, _ := loginAndGetCookie(t, p)

	_, body := getTrashPage(t, p, cookieVal, "/admin/trash")
	if !strings.Contains(body, "3 most recently deleted of 300") {
		t.Error("the page drew 3 of 300 deleted rows and said nothing about the other 297")
	}
}

// A trash row must not link into the resource's detail page: that page reads the
// LIVE rows, so the link 404s — and a 404 from a recovery page reads as "the row
// really is gone".
//
// Falsification: in resource/trash.templ, render the first cell as the anchor
// listRow uses (`<a class="row-name" href={ templ.SafeURL(row.Href) }>`) → RED.
func TestTrash_DoesNotLinkIntoTheLiveDetailPage(t *testing.T) {
	p := newWriterPanel()
	rows := []resource.Row{{
		ID:    "a",
		Cells: []resource.Cell{{Value: "Row-a"}, {Value: "2026-08-20"}},
		Href:  "/admin/items/a",
	}}
	resource.Register(p, trashResource("items", rows, 1, nil))
	cookieVal, _ := loginAndGetCookie(t, p)

	_, body := getTrashPage(t, p, cookieVal, "/admin/trash")
	if strings.Contains(body, `href="/admin/items/a"`) {
		t.Error("the trash linked a deleted row into the live detail page, which 404s")
	}
}

// Both halves of the opt-in are startup mistakes, so both are startup panics.
func TestTrash_RegisterFailsClosed(t *testing.T) {
	t.Run("TrashLister without Restore", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("a trash with no way back registered without complaint; every " +
					"Вернуть button on that section would 403")
			}
		}()
		r := trashResource("items", nil, 0, nil)
		r.Writer.Restore = nil
		resource.Register(newWriterPanel(), r)
	})
	t.Run("TrashLister without Delete", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("a trash nothing can fill registered without complaint")
			}
		}()
		r := trashResource("items", nil, 0, nil)
		r.Writer.Delete = nil
		resource.Register(newWriterPanel(), r)
	})
}

// A resource literally named "trash" would fight the panel-wide page for the
// same path. Fail at startup, where it is one rename, not at runtime.
func TestTrash_NameCollisionPanicsAtStartup(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("a resource named \"trash\" registered alongside the Trash page")
		}
	}()
	p := newWriterPanel()
	resource.Register(p, trashResource("trash", nil, 0, nil))
	p.Handler() // finalize mounts the trash page
}

// The Trash link must sit under a heading of its own.
//
// shell.toNavGroups files a Group-less item under the group that registered
// LAST, and only opens an anonymous bucket when nothing has been grouped yet.
// So a bare append does not put Trash at the bottom of the sidebar — it puts it
// INSIDE the final resource group, where it reads as one of that group's
// resources.
//
// Falsification: in resource/trash.go, delete the
// `p.nav = append(p.nav, shell.NavItem{Group: trashNavGroup})` line → RED.
func TestTrash_SidebarLinkHasItsOwnHeading(t *testing.T) {
	p := newWriterPanel()
	r := trashResource("items", trashRows(1), 1, nil)
	r.Group = "Content"
	resource.Register(p, r)
	p.Handler() // finalize is what mounts the trash nav entry

	items := p.NavItems()
	idx := -1
	for i, it := range items {
		if it.URL == "/admin/trash" {
			idx = i
			break
		}
	}
	if idx < 1 {
		t.Fatalf("trash nav item not found at a groupable position (idx=%d of %d)", idx, len(items))
	}
	if prev := items[idx-1]; prev.Group == "" || prev.URL != "" {
		t.Errorf("the Trash link follows %q (group=%q, url=%q) rather than a group header, "+
			"so it renders inside the last resource group instead of standing on its own",
			prev.Label, prev.Group, prev.URL)
	}
}
