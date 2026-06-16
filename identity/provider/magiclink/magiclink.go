// Package magiclink implements a passwordless email magic-link Provider.
//
// Token design (selector/verifier, keyed-HMAC, single-use):
//   - Start mints a 48-byte crypto/rand token: a 16-byte selector + 32-byte
//     verifier. The token (base64url) is embedded in the emailed link and is the
//     only secret the user holds.
//   - Redis stores ONLY keyed HMACs, never the token: the key is
//     "magic:"+HMAC(selector, pepper); the value is {email, HMAC(verifier, pepper)}.
//   - Verify recomputes HMAC(selector) to find the record via GETDEL (atomic
//     single-use), then constant-time-compares HMAC(verifier) against the stored
//     value with hmac.Equal. A correct selector with a wrong verifier is rejected.
//
// This realizes the design's "store only the HMAC, never the token" while making
// the constant-time verifier check load-bearing: removing it would let a token
// with a guessed/replayed selector but wrong verifier authenticate.
package magiclink

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/anatolykoptev/go-panel/identity/provider"
)

// ErrInvalidToken is returned by Verify for any token that is malformed, expired,
// already used, or whose verifier does not match. The cases are deliberately
// indistinguishable to the caller (no oracle).
var ErrInvalidToken = errors.New("identity/magiclink: invalid or expired token")

const (
	// ProviderName is the registry name and ProviderIdentity.ProviderName.
	ProviderName = "email"
	// MaxTTL caps the magic-record lifetime regardless of configured ttl.
	MaxTTL = 15 * time.Minute

	magicPrefix = "magic:"
	tokenBytes  = 48 // 16-byte selector + 32-byte (256-bit) verifier
	selectorLen = 16
)

// MagicLinkProvider implements provider.Provider plus Start/Verify.
type MagicLinkProvider struct {
	rdb    redis.Cmdable
	pepper []byte
	ttl    time.Duration
}

// magicRecord is the Redis value. VH is stored as base64 by encoding/json since
// it is a []byte; the plaintext verifier is never persisted.
type magicRecord struct {
	Email string `json:"email"`
	VH    []byte `json:"vh"`
}

// New returns a MagicLinkProvider. ttl is clamped to (0, MaxTTL]; a non-positive
// or over-long ttl falls back to MaxTTL so the ≤15min invariant holds structurally.
func New(rdb redis.Cmdable, pepper []byte, ttl time.Duration) *MagicLinkProvider {
	if ttl <= 0 || ttl > MaxTTL {
		ttl = MaxTTL
	}
	cp := make([]byte, len(pepper))
	copy(cp, pepper)
	return &MagicLinkProvider{rdb: rdb, pepper: cp, ttl: ttl}
}

// Name implements provider.Provider.
func (p *MagicLinkProvider) Name() string { return ProviderName }

// Kind implements provider.Provider.
func (p *MagicLinkProvider) Kind() provider.Kind { return provider.KindMagicLink }

// mac returns HMAC-SHA256(data, pepper).
func (p *MagicLinkProvider) mac(data []byte) []byte {
	h := hmac.New(sha256.New, p.pepper)
	h.Write(data)
	return h.Sum(nil)
}

// recordKey is the Redis key for a selector: "magic:"+base64url(HMAC(selector)).
func (p *MagicLinkProvider) recordKey(selector []byte) string {
	return magicPrefix + base64.RawURLEncoding.EncodeToString(p.mac(selector))
}

// Start mints a token, stores its keyed-HMAC record with a capped TTL, and
// returns the raw token to embed in the magic link.
func (p *MagicLinkProvider) Start(ctx context.Context, email string) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("identity/magiclink: rand: %w", err)
	}
	selector, verifier := raw[:selectorLen], raw[selectorLen:]

	rec := magicRecord{Email: email, VH: p.mac(verifier)}
	val, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("identity/magiclink: marshal: %w", err)
	}

	if err := p.rdb.Set(ctx, p.recordKey(selector), val, p.ttl).Err(); err != nil {
		return "", fmt.Errorf("identity/magiclink: store: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Verify consumes the token (single-use) and returns the normalized identity.
func (p *MagicLinkProvider) Verify(ctx context.Context, token string) (provider.ProviderIdentity, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return provider.ProviderIdentity{}, ErrInvalidToken
	}
	selector, verifier := raw[:selectorLen], raw[selectorLen:]

	// GETDEL: atomic read+delete → guaranteed single-use even under concurrent verifies.
	val, err := p.rdb.GetDel(ctx, p.recordKey(selector)).Result()
	switch {
	case errors.Is(err, redis.Nil):
		return provider.ProviderIdentity{}, ErrInvalidToken
	case err != nil:
		return provider.ProviderIdentity{}, fmt.Errorf("identity/magiclink: getdel: %w", err)
	}

	var rec magicRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return provider.ProviderIdentity{}, ErrInvalidToken
	}

	// Constant-time verifier check. Load-bearing: a valid selector with a wrong
	// verifier reaches here, and only hmac.Equal rejects it.
	if !hmac.Equal(p.mac(verifier), rec.VH) {
		return provider.ProviderIdentity{}, ErrInvalidToken
	}

	return provider.ProviderIdentity{
		ProviderName: ProviderName,
		RawUID:       rec.Email,
		Email:        rec.Email,
	}, nil
}
