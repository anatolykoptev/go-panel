package provider_test

import (
	"testing"

	"github.com/anatolykoptev/go-panel/identity/provider"
)

// fakeProvider is a minimal Provider used to exercise the Registry.
type fakeProvider struct {
	name string
	kind provider.Kind
}

func (f fakeProvider) Name() string        { return f.name }
func (f fakeProvider) Kind() provider.Kind { return f.kind }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := provider.NewRegistry()
	p := fakeProvider{name: "email", kind: provider.KindMagicLink}
	r.Register(p)

	got, ok := r.Get("email")
	if !ok {
		t.Fatal("Get(\"email\") returned ok=false after Register")
	}
	if got.Name() != "email" {
		t.Fatalf("Name() = %q, want %q", got.Name(), "email")
	}
	if got.Kind() != provider.KindMagicLink {
		t.Fatalf("Kind() = %v, want %v", got.Kind(), provider.KindMagicLink)
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	r := provider.NewRegistry()
	if _, ok := r.Get("nope"); ok {
		t.Fatal("Get on empty registry returned ok=true")
	}
}

func TestRegistryNames(t *testing.T) {
	r := provider.NewRegistry()
	r.Register(fakeProvider{name: "email", kind: provider.KindMagicLink})
	r.Register(fakeProvider{name: "vk", kind: provider.KindOAuth})

	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("Names() len = %d, want 2", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen["email"] || !seen["vk"] {
		t.Fatalf("Names() = %v, want both email and vk", names)
	}
}

func TestKindString(t *testing.T) {
	cases := map[provider.Kind]string{
		provider.KindMagicLink: "magic_link",
		provider.KindOAuth:     "oauth",
		provider.KindPassword:  "password",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestProviderIdentityFields(t *testing.T) {
	id := provider.ProviderIdentity{
		ProviderName: "email",
		RawUID:       "a@b.com",
		Email:        "a@b.com",
		Phone:        "",
	}
	if id.ProviderName != "email" || id.RawUID != "a@b.com" || id.Email != "a@b.com" {
		t.Fatalf("ProviderIdentity fields not set as expected: %+v", id)
	}
}
