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
	want := tenant.Tenant{CitySlug: "msk", CountryCode: "RU", Locale: "ru"}
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
