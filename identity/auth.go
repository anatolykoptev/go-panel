package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-panel/identity/email"
	"github.com/anatolykoptev/go-panel/identity/provider"
	"github.com/anatolykoptev/go-panel/identity/session"
)

const (
	// DefaultDeviceCookieName is the anonymous device-id cookie read on verify to
	// merge favorites (epid → user). go-grad sets it on first anonymous visit.
	DefaultDeviceCookieName = "epid"

	defaultMagicProviderName = "email"
	defaultLoginPath         = "/login"
	defaultEmailSubject      = "Sign in"
	defaultMagicTTL          = 15 * time.Minute
	defaultSessionTTL        = 30 * 24 * time.Hour

	// verifyPath is the path appended to BaseURL for the emitted magic link.
	verifyPath = "/auth/magic/verify"
)

// MagicProvider is the narrow interface the magic-link handlers consume. The
// concrete *magiclink.MagicLinkProvider satisfies it; the framework resolves it
// from the Registry by name so it never imports the magiclink package directly.
type MagicProvider interface {
	provider.Provider
	Start(ctx context.Context, email string) (token string, err error)
	Verify(ctx context.Context, token string) (provider.ProviderIdentity, error)
}

// Config wires every dependency of the public authenticator. All region-specific
// values (pepper-bound Hasher, Redis-bound stores, SMTP creds, BaseURL) are
// injected by go-grad — the framework holds no package-level region state
// (ADR-001).
type Config struct {
	Registry    *provider.Registry
	Sessions    session.SessionStore
	Users       UserStore
	Email       email.EmailSender
	Hasher      func([]byte) []byte
	RateLimiter RateLimiter
	Cookie      CookieConfig
	BaseURL     string

	MagicTTL   time.Duration
	SessionTTL time.Duration
	EmailRate  RateRule // per-email magic-start throttle
	IPRate     RateRule // per-IP magic-start throttle

	// MagicProviderName selects the registered MagicProvider (default "email").
	MagicProviderName string
	// DeviceCookieName overrides DefaultDeviceCookieName.
	DeviceCookieName string
	// LoginPath is where a failed verify redirects (default "/login").
	LoginPath string
	// EmailSubject is the magic-link email subject (default "Sign in").
	EmailSubject string

	Logger *slog.Logger
}

// PublicAuthenticator composes the framework dependencies and exposes the HTTP
// handler builders. Construct it with New.
type PublicAuthenticator struct {
	cfg   Config
	magic MagicProvider
	log   *slog.Logger
}

// New validates cfg, resolves the magic provider from the registry, applies
// defaults, and returns a ready PublicAuthenticator.
func New(cfg Config) (*PublicAuthenticator, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	name := cfg.MagicProviderName
	if name == "" {
		name = defaultMagicProviderName
	}
	p, ok := cfg.Registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("identity: magic provider %q not registered", name)
	}
	magic, ok := p.(MagicProvider)
	if !ok {
		return nil, fmt.Errorf("identity: provider %q does not implement MagicProvider", name)
	}

	cfg = applyDefaults(cfg)

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &PublicAuthenticator{cfg: cfg, magic: magic, log: log}, nil
}

func validateConfig(cfg Config) error {
	switch {
	case cfg.Registry == nil:
		return errors.New("identity: Config.Registry is required")
	case cfg.Sessions == nil:
		return errors.New("identity: Config.Sessions is required")
	case cfg.Users == nil:
		return errors.New("identity: Config.Users is required")
	case cfg.Email == nil:
		return errors.New("identity: Config.Email is required")
	case cfg.Hasher == nil:
		return errors.New("identity: Config.Hasher is required")
	case cfg.RateLimiter == nil:
		return errors.New("identity: Config.RateLimiter is required")
	case cfg.BaseURL == "":
		return errors.New("identity: Config.BaseURL is required")
	default:
		return nil
	}
}

func applyDefaults(cfg Config) Config {
	if cfg.Cookie.Name == "" {
		cfg.Cookie = DefaultCookieConfig()
	}
	if cfg.DeviceCookieName == "" {
		cfg.DeviceCookieName = DefaultDeviceCookieName
	}
	if cfg.LoginPath == "" {
		cfg.LoginPath = defaultLoginPath
	}
	if cfg.EmailSubject == "" {
		cfg.EmailSubject = defaultEmailSubject
	}
	if cfg.MagicTTL <= 0 {
		cfg.MagicTTL = defaultMagicTTL
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = defaultSessionTTL
	}
	return cfg
}
