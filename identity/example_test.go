package identity_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/anatolykoptev/go-panel/identity"
	"github.com/anatolykoptev/go-panel/identity/email"
	"github.com/anatolykoptev/go-panel/identity/provider"
	"github.com/anatolykoptev/go-panel/identity/provider/magiclink"
	"github.com/anatolykoptev/go-panel/identity/ratelimit"
	"github.com/anatolykoptev/go-panel/identity/session"
	"github.com/redis/go-redis/v9"
)

// memUserStore is a trivial in-memory identity.UserStore — the ONE seam a host
// implements. A production host backs these three methods with its own database.
type memUserStore struct {
	users  map[string]session.UserSnapshot
	byHash map[string]string
	links  map[string]string
	seq    int
}

func newMemUserStore() *memUserStore {
	return &memUserStore{
		users:  map[string]session.UserSnapshot{},
		byHash: map[string]string{},
		links:  map[string]string{},
	}
}

func (m *memUserStore) UpsertIdentity(_ context.Context, _ string, uidHash []byte, contact string) (string, bool, error) {
	if id, ok := m.byHash[string(uidHash)]; ok {
		return id, false, nil
	}
	m.seq++
	id := fmt.Sprintf("user-%d", m.seq)
	m.byHash[string(uidHash)] = id
	m.users[id] = session.UserSnapshot{UserID: id, DisplayName: contact, CitySlug: "spb"}
	return id, true, nil
}

func (m *memUserStore) GetUserSnapshot(_ context.Context, userID string) (session.UserSnapshot, error) {
	snap, ok := m.users[userID]
	if !ok {
		return session.UserSnapshot{}, fmt.Errorf("user %q not found", userID)
	}
	return snap, nil
}

func (m *memUserStore) LinkDevice(_ context.Context, epid, userID string) error {
	if _, ok := m.links[epid]; !ok { // link-once: first claim wins
		m.links[epid] = userID
	}
	return nil
}

// compile-time: the in-memory store satisfies the one seam a host must implement.
var _ identity.UserStore = (*memUserStore)(nil)

// TestExampleWiring is the complete, compile-checked host integration of
// go-panel/identity. Everything except the UserStore comes from the framework. A
// real host swaps memUserStore for a DB-backed store and miniredis for its
// production Redis, and optionally adds promobs + a trusted-proxy ClientIP.
func TestExampleWiring(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// Per-region pepper: >=32 bytes, from a secret store in production.
	pepper := []byte("example-32B+-per-region-auth-pepper-xyz")

	hasher, err := identity.NewProviderUIDHasher(pepper)
	if err != nil {
		t.Fatalf("hasher: %v", err)
	}
	magicProvider, err := magiclink.New(rdb, pepper, 15*time.Minute)
	if err != nil {
		t.Fatalf("magiclink: %v", err)
	}
	registry := provider.NewRegistry()
	registry.Register(magicProvider)

	auth, err := identity.New(identity.Config{
		Registry:    registry,
		Sessions:    session.NewRedisSessionStore(rdb),
		Users:       newMemUserStore(),       // <- the only seam a host writes
		Email:       email.NewLogSender(nil), // prod: email.NewSMTPSender(cfg)
		Hasher:      hasher.Hash,
		RateLimiter: ratelimit.NewRedisLimiter(rdb), // framework battery
		Cookie:      identity.DefaultCookieConfig(),
		BaseURL:     "https://app.example",
		MagicTTL:    15 * time.Minute,
		SessionTTL:  12 * time.Hour,
		EmailRate:   identity.RateRule{Limit: 5, Window: time.Hour},
		IPRate:      identity.RateRule{Limit: 30, Window: time.Hour},
		// Observer: promobs.New(prometheus.DefaultRegisterer, "myapp"), // optional metrics
		// ClientIP: trustedProxyParser, // REQUIRED behind a reverse proxy (see SECURITY.md)
	})
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	if auth == nil {
		t.Fatal("identity.New returned nil authenticator")
	}

	// Mount the framework handlers on any net/http mux.
	mux := http.NewServeMux()
	mux.Handle("POST /auth/magic/start", identity.MagicStartHandler(auth))
	mux.Handle("GET /auth/magic/verify", identity.MagicVerifyHandler(auth))
	mux.Handle("POST /auth/logout", identity.LogoutHandler(auth))
	mux.Handle("POST /auth/device/link", identity.LinkDeviceHandler(auth))
	_ = mux
}
