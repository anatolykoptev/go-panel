package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/anatolykoptev/go-panel/tenant"
)

func TestIsGlobal_TrueForGlobalCitySlug(t *testing.T) {
	if !tenant.IsGlobal(tenant.Tenant{CitySlug: "spb", CountryCode: "RU"}) {
		t.Error("expected spb to be the global tenant")
	}
}

func TestIsGlobal_FalseForNonGlobalCitySlug(t *testing.T) {
	if tenant.IsGlobal(tenant.Tenant{CitySlug: "msk", CountryCode: "RU"}) {
		t.Error("expected msk to NOT be the global tenant")
	}
}

func TestGlobalOnlyAuthorizer_AllowsGlobalTenant(t *testing.T) {
	a := tenant.GlobalOnlyAuthorizer{}
	ok, err := a.Authorized(context.Background(), tenant.Tenant{CitySlug: "spb", CountryCode: "RU"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected GlobalOnlyAuthorizer to allow the global tenant")
	}
}

func TestGlobalOnlyAuthorizer_DeniesNonGlobalTenant(t *testing.T) {
	a := tenant.GlobalOnlyAuthorizer{}
	ok, err := a.Authorized(context.Background(), tenant.Tenant{CitySlug: "msk", CountryCode: "RU"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected GlobalOnlyAuthorizer to deny a non-global tenant — fail-closed default")
	}
}

// TestAuthorizer_InterfaceSatisfiedByGlobalOnly is a compile-time-flavoured
// check that GlobalOnlyAuthorizer actually implements Authorizer (not just a
// struct with a same-shaped method) — falsifiable if the method signature
// drifts from the interface.
func TestAuthorizer_InterfaceSatisfiedByGlobalOnly(t *testing.T) {
	var _ tenant.Authorizer = tenant.GlobalOnlyAuthorizer{}
}

// stubAuthorizer lets a test control the (bool, error) return independently —
// used to prove callers of Authorizer (resource.requireTenant) fail closed on
// error regardless of the bool.
type stubAuthorizer struct {
	ok  bool
	err error
}

func (s stubAuthorizer) Authorized(context.Context, tenant.Tenant) (bool, error) {
	return s.ok, s.err
}

func TestStubAuthorizer_ReturnsConfiguredValues(t *testing.T) {
	want := errors.New("boom")
	s := stubAuthorizer{ok: true, err: want}
	ok, err := s.Authorized(context.Background(), tenant.Tenant{CitySlug: "spb"})
	if ok != true || !errors.Is(err, want) {
		t.Fatalf("got (%v, %v), want (true, %v)", ok, err, want)
	}
}
