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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
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

// ---------------------------------------------------------------------------
// Round 1 of review on PR #132. Every test below pins a line that was CORRECT
// but unpinned: the reviewer mutated each one and the package stayed green.
// Enforcement is exactly the part that fails silently, so it is the part that
// most needs a mutant.
// ---------------------------------------------------------------------------

// The Trash aggregates several resources onto one URL, so a single unguarded
// route exposes all of them at once. The shipped code guards it; nothing made
// that fact observable.
//
// Falsification: in resource/trash.go mountTrash, replace
// `p.guard("", p.handleTrash)` with `http.HandlerFunc(p.handleTrash)` → RED.
// Measured before this test existed: that edit left the package green while an
// unauthenticated GET returned 200 with the row bodies.
func TestTrash_RequiresASession(t *testing.T) {
	p := newWriterPanel()
	rows := []resource.Row{{
		ID:    "a",
		Cells: []resource.Cell{{Value: "SECRET-ROW"}, {Value: "2026-08-20"}},
	}}
	resource.Register(p, trashResource("items", rows, 1, nil))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/trash", nil)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req) // no session cookie

	if w.Code == http.StatusOK {
		t.Errorf("unauthenticated GET /admin/trash answered 200 — one unguarded URL dumps the "+
			"deleted rows of EVERY opted-in resource; body: %.120s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SECRET-ROW") {
		t.Error("a deleted row leaked to a request carrying no session")
	}
}

// The tenant on the request must reach the consumer's TrashLister. This is the
// likeliest consumer mistake by construction: Lister usually reads a view that
// already carries the tenant predicate, while TrashLister reads the base table
// on purpose and therefore drops exactly that scoping.
//
// Falsification: in resource/trash.go handleTrash, replace
// `Tenant: tenant.From(ctx)` with `Tenant: tenant.Tenant{}` → RED. The panel
// must be tenant-configured for the mutant to be visible at all: under the
// default resolver the real tenant IS the zero value, and no assertion can tell
// the two apart.
func TestTrash_PassesTheRequestTenantToTheLister(t *testing.T) {
	p := newTenantTestPanel(allowCityAuthorizer{allowed: "spb"})
	var got tenant.Tenant
	r := trashResource("items", trashRows(1), 1, nil)
	r.TrashLister = func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		got = q.Tenant
		return trashRows(1), 1, nil
	}
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	if code, _ := getTrashPage(t, p, cookieVal, "/admin/tenant/spb/trash"); code != http.StatusOK {
		t.Fatalf("GET the tenant-scoped trash: %d", code)
	}
	if got.CitySlug != "spb" {
		t.Errorf("TrashLister was handed tenant %+v, want CitySlug \"spb\" — a lister that "+
			"trusts this field would read every tenant's deleted rows", got)
	}
}

// Visible is cosmetic by contract, but it is the second filter in
// trashResourcesFor and the first one had the only test between them.
//
// Falsification: in resource/trash.go trashResourcesFor, replace
// `if r.Visible != nil && !r.Visible(ctx)` with
// `if false && r.Visible != nil && !r.Visible(ctx)` → RED.
func TestTrash_HonoursTheVisiblePredicate(t *testing.T) {
	p := newWriterPanel()
	shown := trashResource("items", trashRows(1), 1, nil)
	hidden := trashResource("invoices", []resource.Row{
		{ID: "z", Cells: []resource.Cell{{Value: "HIDDEN-ROW"}, {Value: "2026-08-20"}}},
	}, 1, nil)
	hidden.Visible = func(context.Context) bool { return false }
	resource.Register(p, shown)
	resource.Register(p, hidden)
	cookieVal, _ := loginAndGetCookie(t, p)

	_, body := getTrashPage(t, p, cookieVal, "/admin/trash")
	if !strings.Contains(body, "Row-a") {
		t.Error("the visible resource's trashed row is missing")
	}
	if strings.Contains(body, "HIDDEN-ROW") {
		t.Error("a resource hidden by Visible still contributed rows to the trash")
	}
}

// A consumer that already mounted its own /admin/trash page must not lose it on
// a version bump. net/http rejects only a byte-identical pattern, so
// MountPage's "trash/{$}" and the Trash's bare "/trash" coexist in the mux and
// the consumer's page is silently shadowed — the one consumer most likely to
// have a trash page already is the one this would surprise.
//
// Falsification: in resource/trash.go mountTrash, delete the `p.pagePaths`
// loop → RED (and, measured, the consumer's page keeps answering only on the
// trailing-slash URL while /admin/trash serves this page instead).
func TestTrash_CollidingMountPagePanicsAtStartup(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec resource.PageSpec
	}{
		{"path", resource.PageSpec{Path: "trash", Handler: func(http.ResponseWriter, *http.Request) {}}},
		{"alias", resource.PageSpec{
			Path:    "reports",
			Aliases: []string{"trash"},
			Handler: func(http.ResponseWriter, *http.Request) {},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("a MountPage %s of %q survived alongside the Trash page — the "+
						"consumer's page stops answering its own URL with no error anywhere",
						tc.name, "trash")
				}
				// net/http's own duplicate-pattern panic would also be a panic,
				// and it does NOT fire for the Path case — assert on our message
				// so the test cannot pass on the wrong one.
				if msg := fmt.Sprint(r); !strings.Contains(msg, "MountPage path or alias") {
					t.Errorf("panicked with %q, not the Trash collision message", msg)
				}
			}()
			p := newWriterPanel()
			p.MountPage(tc.spec)
			resource.Register(p, trashResource("items", nil, 0, nil))
			p.Handler()
		})
	}
}

// The Trash link joins the "System" group through the same insertion a resource
// uses, so a consumer that already has a System group gets ONE heading.
//
// Falsification: in resource/trash.go mountTrash, replace the `addNavLink(...)`
// call with the two bare `p.nav = append(...)` statements it replaced → RED
// (measured: systemHeadings=2).
func TestTrash_ReusesAnExistingSystemGroupHeading(t *testing.T) {
	p := newWriterPanel()
	consumer := undoResourceBare()
	consumer.Name = "audit"
	consumer.Title = "Audit"
	consumer.Group = "System"
	resource.Register(p, consumer)
	resource.Register(p, trashResource("items", trashRows(1), 1, nil))
	p.Handler()

	headings := 0
	for _, it := range p.NavItems() {
		if it.Group == "System" && it.URL == "" {
			headings++
		}
	}
	if headings != 1 {
		t.Errorf("sidebar carries %d \"System\" headings, want 1 — the Trash opened its own "+
			"beside the consumer's identically-named group", headings)
	}
}

// A heading with every member filtered away is not a heading. The Trash is the
// first nav item whose whole group can vanish for one operator, and a bare
// caption over nothing reads as a section that failed to load.
//
// Asserted in both directions: an operator who holds the role sees the heading,
// one who does not sees neither the link nor the caption. A one-directional
// absence test would also pass if the heading never rendered at all.
//
// Falsification: in resource/resource.go navItemsFor, drop the
// `if !groupHasVisibleItem(...) { continue }` guard → RED.
func TestTrash_EmptyGroupHeadingIsDropped(t *testing.T) {
	sidebarFor := func(t *testing.T, holdsRole bool) string {
		t.Helper()
		a := newTrashRoleAuth(map[string]bool{"editor": holdsRole})
		p := resource.New(resource.Config{
			Title: "Test Panel", BasePath: "/admin", Auth: a, CSRFKey: testCSRFKey,
		})
		// An ungated resource so this operator always has a page to land on.
		resource.Register(p, undoResourceBare())
		gated := trashResource("invoices", trashRows(1), 1, nil)
		gated.RequiredRole = "editor"
		resource.Register(p, gated)
		cookieVal, _ := loginAndGetCookie(t, p)
		code, body := getTrashPage(t, p, cookieVal, "/admin/items")
		if code != http.StatusOK {
			t.Fatalf("GET /admin/items: %d", code)
		}
		return body
	}

	if body := sidebarFor(t, true); !strings.Contains(body, trashNavGroupLabel) {
		t.Fatalf("an operator who CAN reach the trash sees no %q heading — the test below "+
			"would then pass for the wrong reason", trashNavGroupLabel)
	}
	if body := sidebarFor(t, false); strings.Contains(body, trashNavGroupLabel) {
		t.Errorf("the sidebar shows a %q heading with nothing under it: this operator can reach "+
			"none of the opted-in resources, so the Trash link is hidden and only its caption "+
			"remains", trashNavGroupLabel)
	}
}

// trashNavGroupLabel mirrors resource.trashNavGroup, which is unexported.
const trashNavGroupLabel = "System"

// ---------------------------------------------------------------------------
// Round 2 of review. The empty-heading rule from round 1 was right in intent
// and wrong in its matching: it found a header by ID, which is what Register
// stamps, and NOT what AddNav's own doc tells consumers to build. The two tests
// below pin the corrected rule and the cost of evaluating it, because both
// failures land on consumers who never asked for a Trash.
// ---------------------------------------------------------------------------

// A hand-rolled group header — the shape AddNav documents, carrying no ID —
// must survive when its members do.
//
// Falsification: in resource/resource.go, change isNavHeader to match by ID
// (`item.ID == "group:"+item.Group`) → RED. Measured before this test existed:
// the heading vanished and its link was absorbed into the PRECEDING, unrelated
// group, in every panel, with or without a Trash.
func TestNav_HandRolledGroupHeaderSurvives(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, undoResourceBare())
	// Exactly the shape AddNav's doc prescribes: bare Group, no ID.
	p.AddNav(shell.NavItem{Group: "ZZReports"})
	p.AddNav(shell.NavItem{ID: "zzdocs", Label: "ZZDocs", URL: "/admin/zzdocs"})
	cookieVal, _ := loginAndGetCookie(t, p)

	_, body := getTrashPage(t, p, cookieVal, "/admin/items")
	if !strings.Contains(body, "ZZReports") {
		t.Error("a group header built the way AddNav documents was dropped from the sidebar; " +
			"its link then renders under whichever unrelated group came before it")
	}
	if !strings.Contains(body, "ZZDocs") {
		t.Error("the hand-rolled group's link is missing entirely")
	}
}

// Visible is consumer code and has no caching helper — unlike Badge, whose doc
// tells you to wrap it in shell.CachedBadge. Deciding a header from its members
// must not mean re-running every member's closure per header.
//
// Falsification: in resource/resource.go navItemsFor, call navLinkVisible from
// headerHasVisibleMember instead of reading the precomputed slice → RED.
// Measured on the round-1 tip: 4 calls per render where main made 1.
func TestNav_VisibleIsEvaluatedOncePerRender(t *testing.T) {
	var calls int
	r := undoResourceBare() // no TrashLister: this panel has no Trash at all
	r.Visible = func(context.Context) bool { calls++; return true }
	p := newWriterPanel()
	resource.Register(p, r)
	cookieVal, _ := loginAndGetCookie(t, p)

	calls = 0
	if code, _ := getTrashPage(t, p, cookieVal, "/admin/items"); code != http.StatusOK {
		t.Fatalf("GET /admin/items: %d", code)
	}
	if calls != 1 {
		t.Errorf("Visible was called %d times rendering one page, want 1 — a consumer whose "+
			"predicate costs anything pays that multiplier on every render of every page", calls)
	}
}

// finalize sets p.finalized BEFORE mountTrash, which can panic. sync.Once marks
// the call done even on panic, so a consumer that recovers one must not be left
// with a Panel that still accepts mounts nothing will ever read.
//
// Falsification: in resource/page.go finalize, move `p.finalized = true` back
// after `p.mountTrash()` → RED.
func TestTrash_MountPageIsRefusedAfterARecoveredCollisionPanic(t *testing.T) {
	p := newWriterPanel()
	p.MountPage(resource.PageSpec{Path: "trash", Handler: func(http.ResponseWriter, *http.Request) {}})
	resource.Register(p, trashResource("items", nil, 0, nil))

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the trash/page collision to panic")
			}
		}()
		p.Handler()
	}()

	// The mux is frozen now — finalizeOnce will not run again.
	defer func() {
		if recover() == nil {
			t.Error("MountPage was accepted after a recovered Handler() panic: the route would " +
				"never be mounted and nothing anywhere would say so")
		}
	}()
	p.MountPage(resource.PageSpec{Path: "later", Handler: func(http.ResponseWriter, *http.Request) {}})
}
