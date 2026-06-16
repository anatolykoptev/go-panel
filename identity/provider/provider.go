// Package provider defines the pluggable identity-provider seam for go-panel's
// public-auth framework. A Provider authenticates an external identity claim and
// returns a normalized ProviderIdentity. Concrete providers (magic-link now;
// VK ID / Yandex ID / Google / Apple later) live in subpackages and are wired
// into a Registry per region by go-grad.
//
// The Provider interface is intentionally minimal (Name + Kind). Flow-specific
// methods (e.g. magic-link Start/Verify) live on the concrete type and are
// consumed through narrower interfaces at the call site.
package provider

import "sync"

// Kind classifies how a provider authenticates. Only KindMagicLink is
// implemented today; KindOAuth and KindPassword are reserved for Phase 2+.
type Kind int

const (
	// KindMagicLink is a passwordless email-link flow.
	KindMagicLink Kind = iota + 1
	// KindOAuth is a third-party OAuth/OIDC flow (VK ID, Yandex ID, Google, Apple).
	KindOAuth
	// KindPassword is a classic password flow (US/Phase-4, argon2id).
	KindPassword
)

// String returns the stable, lower-snake-case name of the Kind. The values are
// used in metric labels and logs, so they must not change.
func (k Kind) String() string {
	switch k {
	case KindMagicLink:
		return "magic_link"
	case KindOAuth:
		return "oauth"
	case KindPassword:
		return "password"
	default:
		return "unknown"
	}
}

// ProviderIdentity is the normalized identity a provider returns after a
// successful flow. RawUID is the raw, unhashed external identifier (an email
// address for magic-link, a VK user id for VK OAuth) — callers hash it with the
// per-region pepper before it touches storage (see ADR-002). Email/Phone are
// optional convenience fields a provider may populate.
type ProviderIdentity struct {
	ProviderName string
	RawUID       string
	Email        string
	Phone        string
}

// Provider authenticates an identity claim. Each provider has a unique name
// (e.g. "email", "vk", "google") and a Kind.
type Provider interface {
	Name() string
	Kind() Kind
}

// Registry is a concurrency-safe name→Provider map, wired per region by go-grad.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds p under its Name, replacing any existing provider with the same
// name. A nil provider is ignored.
func (r *Registry) Register(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns the provider registered under name, and whether it was found.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// Names returns the registered provider names in unspecified order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}
