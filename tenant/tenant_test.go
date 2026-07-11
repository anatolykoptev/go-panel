package tenant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-panel/tenant"
)

func TestFrom_DefaultWhenNoTenantInCtx(t *testing.T) {
	got := tenant.From(context.Background())
	if got.CitySlug != "spb" {
		t.Errorf("expected default spb, got %q", got.CitySlug)
	}
}

func TestWithTenant_RoundTrip(t *testing.T) {
	want := tenant.Tenant{CitySlug: "msk", CountryCode: "RU"}
	ctx := tenant.WithTenant(context.Background(), want)
	got := tenant.From(ctx)
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestScopeClause_Global_ReturnsEmpty(t *testing.T) {
	cond, arg := tenant.ScopeClause(tenant.Global, tenant.Tenant{CitySlug: "spb"}, 1)
	if cond != "" || arg != nil {
		t.Errorf("expected empty for global scope, got %q %v", cond, arg)
	}
}

func TestScopeClause_Scoped(t *testing.T) {
	s := tenant.Scope{Column: "p.city_slug"}
	cond, arg := tenant.ScopeClause(s, tenant.Tenant{CitySlug: "spb"}, 1)
	if cond != "p.city_slug = $1" {
		t.Errorf("got %q", cond)
	}
	if arg != "spb" {
		t.Errorf("got arg %v", arg)
	}
}

func TestScopeClause_ArgOffset(t *testing.T) {
	s := tenant.Scope{Column: "e.city_slug"}
	cond, _ := tenant.ScopeClause(s, tenant.Tenant{CitySlug: "spb"}, 3)
	if cond != "e.city_slug = $3" {
		t.Errorf("got %q", cond)
	}
}

func TestPathResolver_ExtractsSlug(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/tenant/msk/entities", nil)
	pr := tenant.PathResolver{Segment: 2}
	got := pr.Resolve(r)
	if got.CitySlug != "msk" {
		t.Errorf("expected msk, got %q", got.CitySlug)
	}
}

func TestPathResolver_FallsBackToDefault(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	pr := tenant.PathResolver{Segment: 2}
	got := pr.Resolve(r)
	if got.CitySlug != "spb" {
		t.Errorf("expected spb default, got %q", got.CitySlug)
	}
}

func TestSubdomainResolver_ExtractsSubdomain(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r.Host = "spb.piter.now"
	sr := tenant.SubdomainResolver{}
	got := sr.Resolve(r)
	if got.CitySlug != "spb" {
		t.Errorf("expected spb, got %q", got.CitySlug)
	}
}

func TestSubdomainResolver_NoSubdomainFallback(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/", nil)
	r.Host = "piter.now"
	sr := tenant.SubdomainResolver{}
	got := sr.Resolve(r)
	if got.CitySlug != "spb" {
		t.Errorf("expected default spb, got %q", got.CitySlug)
	}
}

// TestPathResolver_IgnoresNonTenantSegments locks the 2026-06-11 class: the
// resolver must NOT treat arbitrary path segments at the configured index as a
// tenant slug. /admin/{resource}/new and /admin/{resource}/{id}/edit resolved
// to city "new"/"{id}", breaking every city-scoped query behind a form.
func TestPathResolver_IgnoresNonTenantSegments(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	for _, path := range []string{
		"/admin/rating_sponsorships/new",
		"/admin/rating_segments/5/edit",
		"/admin/rating_segments/5/save",
		"/admin/places/rows",
	} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		if got := pr.Resolve(r); got.CitySlug != "spb" {
			t.Errorf("%s: expected global default spb, got %q", path, got.CitySlug)
		}
	}
}

// TestPathResolver_StripPrefix_RemovesMarkerAndSlug verifies the happy path:
// the /tenant/{slug} segment pair is removed, leaving the rest of the path
// intact.
func TestPathResolver_StripPrefix_RemovesMarkerAndSlug(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	got, changed := pr.StripPrefix("/admin/tenant/msk/entities")
	if !changed {
		t.Fatal("expected changed=true when the marker+slug are present")
	}
	if got != "/admin/entities" {
		t.Errorf("got %q, want /admin/entities", got)
	}
}

// TestPathResolver_StripPrefix_MinimalPath verifies stripping when the slug
// is the last segment (nothing follows it).
func TestPathResolver_StripPrefix_MinimalPath(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	got, changed := pr.StripPrefix("/admin/tenant/msk")
	if !changed {
		t.Fatal("expected changed=true")
	}
	if got != "/admin" {
		t.Errorf("got %q, want /admin", got)
	}
}

// TestPathResolver_StripPrefix_PreservesPathUnchangedWhenMarkerAbsent proves
// the no-op branch returns the ORIGINAL path byte-for-byte (not a trimmed or
// otherwise normalised version) — StripPrefix must never mutate what it does
// not own.
func TestPathResolver_StripPrefix_PreservesPathUnchangedWhenMarkerAbsent(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	const in = "/admin/entities/"
	got, changed := pr.StripPrefix(in)
	if changed {
		t.Fatal("expected changed=false when no /tenant/{slug} marker is present")
	}
	if got != in {
		t.Errorf("got %q, want the input unchanged %q", got, in)
	}
}

// TestPathResolver_StripPrefix_EmptySlugFallsBack mirrors Resolve's own
// slug!="" guard: a marker with an empty slug segment must not strip.
func TestPathResolver_StripPrefix_EmptySlugFallsBack(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	const in = "/admin/tenant//entities"
	got, changed := pr.StripPrefix(in)
	if changed {
		t.Fatal("expected changed=false for an empty slug segment")
	}
	if got != in {
		t.Errorf("got %q, want the input unchanged %q", got, in)
	}
}

// TestPathResolver_StripPrefix_IgnoresNonTenantSegments is the StripPrefix
// twin of TestPathResolver_IgnoresNonTenantSegments: the exact same 2026-06-11
// path shapes must no-op at the strip level too, proving Resolve and
// StripPrefix share one marker guard by construction rather than two
// independently-maintained checks that could drift apart.
func TestPathResolver_StripPrefix_IgnoresNonTenantSegments(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	for _, path := range []string{
		"/admin/rating_sponsorships/new",
		"/admin/rating_segments/5/edit",
		"/admin/rating_segments/5/save",
		"/admin/places/rows",
	} {
		got, changed := pr.StripPrefix(path)
		if changed || got != path {
			t.Errorf("%s: expected no-op (changed=false, path unchanged), got (%q, %v)", path, got, changed)
		}
	}
}

// TestPathResolver_StripPrefix_IdempotentOnAlreadyStrippedPath verifies that
// calling StripPrefix on its own output is a no-op — required for the
// Handler-level wrap to be safely idempotent under the Phase 1a/1b rollout
// interim (see tenant package doc).
func TestPathResolver_StripPrefix_IdempotentOnAlreadyStrippedPath(t *testing.T) {
	pr := tenant.PathResolver{Segment: 2}
	once, changed := pr.StripPrefix("/admin/tenant/msk/entities")
	if !changed {
		t.Fatal("first strip should have changed the path")
	}
	twice, changedAgain := pr.StripPrefix(once)
	if changedAgain {
		t.Fatal("second strip on already-stripped path should be a no-op")
	}
	if twice != once {
		t.Errorf("got %q, want %q unchanged", twice, once)
	}
}
