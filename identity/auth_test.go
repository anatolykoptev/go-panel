package identity_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/anatolykoptev/go-panel/identity"
	"github.com/anatolykoptev/go-panel/identity/provider"
	"github.com/anatolykoptev/go-panel/identity/provider/magiclink"
	"github.com/anatolykoptev/go-panel/identity/session"
)

// newTestConfig builds a minimally-valid identity.Config against a real
// miniredis instance, with logger overridable per test.
func newTestConfig(t *testing.T, logger *slog.Logger) identity.Config {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	ml, err := magiclink.New(rdb, []byte(testPepper), 10*time.Minute)
	if err != nil {
		t.Fatalf("magiclink.New: %v", err)
	}
	reg := provider.NewRegistry()
	reg.Register(ml)

	return identity.Config{
		Registry:    reg,
		Sessions:    session.NewRedisSessionStore(rdb),
		Users:       &fakeUserStore{userID: "user-1", created: true},
		Email:       &fakeEmail{},
		Hasher:      newHasher(t, testPepper).Hash,
		RateLimiter: &fakeRateLimiter{allow: true},
		Cookie:      identity.DefaultCookieConfig(),
		BaseURL:     baseURL,
		MagicTTL:    10 * time.Minute,
		SessionTTL:  time.Hour,
		EmailRate:   identity.RateRule{Limit: 5, Window: 15 * time.Minute},
		IPRate:      identity.RateRule{Limit: 20, Window: 15 * time.Minute},
		Logger:      logger,
	}
}

// TestNew_WarnsWhenObserverUnset verifies that constructing a
// PublicAuthenticator without Config.Observer emits a one-time slog.Warn
// naming the disabled-metrics condition, via the resolved logger (honouring
// Config.Logger when set — not just the global slog.Default()).
// Falsifiability: removing the warning call, or checking the wrong buffer,
// leaves buf empty and the strings.Contains assertion fails.
func TestNew_WarnsWhenObserverUnset(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := newTestConfig(t, logger)
	// Observer intentionally left unset (zero value).
	if _, err := identity.New(cfg); err != nil {
		t.Fatalf("identity.New: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Config.Observer not set") {
		t.Fatalf("expected a warning about unset Observer, got log output: %q", out)
	}
}

// TestNew_NoWarnWhenObserverSet verifies that an explicitly configured
// Observer suppresses the warning.
func TestNew_NoWarnWhenObserverSet(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	cfg := newTestConfig(t, logger)
	cfg.Observer = &recordingObserver{}
	if _, err := identity.New(cfg); err != nil {
		t.Fatalf("identity.New: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Config.Observer not set") {
		t.Fatalf("did not expect the unset-Observer warning when Observer is configured, got: %q", out)
	}
}
