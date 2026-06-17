# Security

go-panel ships an authentication framework (`identity/`). Treat security reports
as confidential.

## Reporting a vulnerability

Do **not** open a public issue for a security problem. Contact the maintainer
privately (operator channel). Include repro steps, affected version (`git tag` /
go module version), and impact. You will get an acknowledgement and a fix
timeline.

## Supported versions

Only the latest minor (`v0.x`) receives security fixes. Pin a tagged release, not
a SHA pseudo-version.

## Security model (identity framework)

- **Cookie:** exact-host (no `Domain=` wildcard), `HttpOnly`, `Secure`,
  `SameSite=Lax`. Serve auth over HTTPS only.
- **Session:** opaque 256-bit id, stored server-side (Redis). Rotated on every
  login (fixation defense); revoke = delete.
- **Magic-link token:** 32 bytes from crypto/rand, single-use (atomic GETDEL),
  TTL capped at 15 min, constant-time compare.
- **provider_uid_hmac:** `HMAC-SHA256(provider_uid, pepper)` — never the plaintext
  provider id. Pepper is per-region, **≥32 bytes**, injected from a secret store.
- **Rate limiter:** fails **closed** (a Redis error denies, never allows).

## Integrator requirements (MUST — the framework cannot enforce these for you)

1. **Override `Config.ClientIP` behind a reverse proxy.** The default uses
   `RemoteAddr`; behind nginx/Caddy every request carries the proxy IP and the
   per-IP throttle collapses into one shared bucket (ineffective limit / accidental
   DoS). Provide a trusted-hop `X-Forwarded-For`/`X-Real-IP` parser.
2. **Provide a strong pepper:** ≥32 bytes, per-region, from a secret store. Never
   commit it. Rotating it invalidates all existing identity hashes (one-way).
3. **`LinkDevice` is link-once on the FIRST user**, but the device id (`epid`) is
   supplied by the caller. Your `UserStore.LinkDevice` MUST only bind an `epid`
   the caller **demonstrably owns** (derive it from the caller's own device
   context, not from an arbitrary client-supplied value). Letting an authenticated
   caller link an arbitrary `epid` lets them claim another user's not-yet-linked
   device association (CWE-639 class). The framework's link-once upsert blocks
   *re-binding an already-owned* epid; the *first-claim* of an unlinked epid is the
   host's responsibility.
4. **Serve over HTTPS** (the `Secure` cookie is dropped on plain HTTP).

## Standards alignment

The session and authentication design targets OWASP ASVS v5 V2 (authentication)
and V3 (session management): server-side opaque sessions, rotation on login,
idle/absolute expiry via Redis TTL, anti-enumeration on the start endpoint, and
rate-limiting on credential-adjacent endpoints.
