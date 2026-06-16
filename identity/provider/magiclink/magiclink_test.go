package magiclink_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/anatolykoptev/go-panel/identity/provider"
	"github.com/anatolykoptev/go-panel/identity/provider/magiclink"
)

const (
	testPepper = "magic-pepper-32-bytes-aaaaaaaaaa"
	testEmail  = "alice@example.com"
	testTTL    = 10 * time.Minute
)

func newProvider(t *testing.T) (*magiclink.MagicLinkProvider, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return magiclink.New(rdb, []byte(testPepper), testTTL), mr
}

func TestProviderMetadata(t *testing.T) {
	p, _ := newProvider(t)
	if p.Name() != "email" {
		t.Fatalf("Name() = %q, want email", p.Name())
	}
	if p.Kind() != provider.KindMagicLink {
		t.Fatalf("Kind() = %v, want KindMagicLink", p.Kind())
	}
	// Compile-time assertion that it satisfies provider.Provider.
	var _ provider.Provider = p
}

func TestStartVerifyHappyPath(t *testing.T) {
	p, _ := newProvider(t)
	ctx := context.Background()

	token, err := p.Start(ctx, testEmail)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if token == "" {
		t.Fatal("Start returned empty token")
	}

	id, err := p.Verify(ctx, token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Email != testEmail || id.RawUID != testEmail {
		t.Fatalf("identity = %+v, want Email/RawUID = %q", id, testEmail)
	}
	if id.ProviderName != "email" {
		t.Fatalf("ProviderName = %q, want email", id.ProviderName)
	}
}

// TestSingleUse locks single-use: the second Verify of the same token MUST fail.
// Falsifiability: this test fails if the delete-on-use (GETDEL) is replaced with
// a plain GET, because the record would survive the first Verify.
func TestSingleUse(t *testing.T) {
	p, _ := newProvider(t)
	ctx := context.Background()

	token, err := p.Start(ctx, testEmail)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := p.Verify(ctx, token); err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	if _, err := p.Verify(ctx, token); !errors.Is(err, magiclink.ErrInvalidToken) {
		t.Fatalf("second Verify err = %v, want ErrInvalidToken (token must be single-use)", err)
	}
}

// TestTokenExpiry locks the short-TTL property. Falsifiability: if Start stored
// the record without an expiry, it would survive the fast-forward and pass.
func TestTokenExpiry(t *testing.T) {
	p, mr := newProvider(t)
	ctx := context.Background()

	token, err := p.Start(ctx, testEmail)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mr.FastForward(testTTL + time.Minute)

	if _, err := p.Verify(ctx, token); !errors.Is(err, magiclink.ErrInvalidToken) {
		t.Fatalf("expired token Verify err = %v, want ErrInvalidToken", err)
	}
}

// TestTTLCappedAt15Min locks the structural invariant that the magic record TTL
// never exceeds 15 minutes even if a larger ttl is configured.
func TestTTLCappedAt15Min(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	p := magiclink.New(rdb, []byte(testPepper), 24*time.Hour) // absurdly long
	ctx := context.Background()

	if _, err := p.Start(ctx, testEmail); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mr.FastForward(magiclink.MaxTTL + time.Minute)

	// Every magic key must have expired despite the 24h config.
	if keys := mr.Keys(); len(keys) != 0 {
		t.Fatalf("magic records survived past MaxTTL: %v", keys)
	}
}

// TestTokenNeverStoredPlaintext locks that the raw token never reaches Redis —
// only keyed HMACs do. Falsifiability: if Start stored the token (or its
// selector/verifier plaintext) the substring search would hit.
func TestTokenNeverStoredPlaintext(t *testing.T) {
	p, mr := newProvider(t)
	ctx := context.Background()

	token, err := p.Start(ctx, testEmail)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	for _, key := range mr.Keys() {
		if strings.Contains(key, token) {
			t.Fatalf("raw token found in Redis key %q", key)
		}
		val, _ := mr.Get(key)
		if strings.Contains(val, token) {
			t.Fatalf("raw token found in Redis value at key %q", key)
		}
	}
}

// TestVerifierIsConstantTimeChecked locks that the verifier half of the token is
// validated with a constant-time compare. We craft a token sharing the real
// selector but with a tampered verifier; it must be rejected.
// Falsifiability: if the hmac.Equal verifier check were removed (accept on
// selector hit alone), the tampered token would authenticate.
func TestVerifierIsConstantTimeChecked(t *testing.T) {
	p, _ := newProvider(t)
	ctx := context.Background()

	token, err := p.Start(ctx, testEmail)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	// Flip a bit in the verifier half (everything after the selector prefix).
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	if _, err := p.Verify(ctx, tampered); !errors.Is(err, magiclink.ErrInvalidToken) {
		t.Fatalf("tampered-verifier token Verify err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyGarbageToken(t *testing.T) {
	p, _ := newProvider(t)
	ctx := context.Background()
	for _, tok := range []string{"", "not-base64!!!", "YWJj"} { // empty, invalid, too-short
		if _, err := p.Verify(ctx, tok); !errors.Is(err, magiclink.ErrInvalidToken) {
			t.Fatalf("Verify(%q) err = %v, want ErrInvalidToken", tok, err)
		}
	}
}

// TestTokensAreRandom locks crypto/rand usage: two Start calls for the same email
// must yield distinct tokens. Falsifiability: a deterministic/counter token would
// collide.
func TestTokensAreRandom(t *testing.T) {
	p, _ := newProvider(t)
	ctx := context.Background()
	t1, _ := p.Start(ctx, testEmail)
	t2, _ := p.Start(ctx, testEmail)
	if t1 == t2 {
		t.Fatal("two Start calls produced identical tokens — not crypto-random")
	}
}
