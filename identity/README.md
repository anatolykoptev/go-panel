# identity — passwordless auth framework

Reusable, host-agnostic authentication for Go services: magic-link login, opaque
Redis sessions, a pluggable provider registry (add VK/Yandex/Google later),
pepper-keyed identity hashing, exact-host secure cookies, and ready HTTP handlers.

A host wires the framework and implements exactly **one** seam — `UserStore` (over
its own database). Everything else is provided.

## What the framework gives you

| Piece | Package |
|---|---|
| Magic-link provider | `identity/provider/magiclink` |
| Opaque Redis sessions (rotate-on-login) | `identity/session` |
| Redis rate limiter (fail-closed) | `identity/ratelimit` |
| SMTP + dev-log email senders | `identity/email` |
| Prometheus observer (optional) | `identity/promobs` |
| Secure cookie config | `identity.DefaultCookieConfig()` |
| Pepper-keyed provider-uid HMAC | `identity.NewProviderUIDHasher` |
| HTTP handlers (start / verify / logout / device-link) | `identity.*Handler` |

## What you implement

One seam: `identity.UserStore` (3 methods — `UpsertIdentity`, `GetUserSnapshot`,
`LinkDevice`) over your DB.

## Wiring

See `example_test.go` (`TestExampleWiring`) — a complete, compile-checked host
wiring with an in-memory `UserStore`. Distilled:

```go
auth, _ := identity.New(identity.Config{
    Registry:    registry,                          // magiclink registered
    Sessions:    session.NewRedisSessionStore(rdb),
    Users:       myDBStore,                          // <- the only seam you write
    Email:       email.NewSMTPSender(smtpCfg),
    Hasher:      hasher.Hash,
    RateLimiter: ratelimit.NewRedisLimiter(rdb),
    Cookie:      identity.DefaultCookieConfig(),
    BaseURL:     "https://app.example",
    MagicTTL:    15 * time.Minute,
    SessionTTL:  12 * time.Hour,
    EmailRate:   identity.RateRule{Limit: 5, Window: time.Hour},
    IPRate:      identity.RateRule{Limit: 30, Window: time.Hour},
    ClientIP:    trustedProxyParser, // REQUIRED behind a reverse proxy
})
mux.Handle("POST /auth/magic/start",  identity.MagicStartHandler(auth))
mux.Handle("GET /auth/magic/verify",  identity.MagicVerifyHandler(auth))
mux.Handle("POST /auth/logout",       identity.LogoutHandler(auth))
mux.Handle("POST /auth/device/link",  identity.LinkDeviceHandler(auth))
```

## Security

Read [`../SECURITY.md`](../SECURITY.md) before deploying — especially the
**integrator requirements**: override `ClientIP` behind a proxy; pepper >=32 bytes
per-region from a secret store; the `LinkDevice` possession contract; HTTPS-only.
