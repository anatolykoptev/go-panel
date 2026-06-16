# Identity Framework Design

## Overview

Multi-role public authentication for the piter.now / city-guide platform. Two sibling systems live in go-panel — operator auth (`go-panel/auth`, existing HMACAuth) and public auth (`go-panel/identity`, this doc). They never share code paths, cookies, or stores.

**Scope of this document:** `go-panel/identity` — the reusable framework. Per-region storage lives in `go-grad/identitystore` (see `go-grad/docs/identity-auth-storage.md`).

## Goals and Non-Goals

Goals:
- Passwordless magic-link email login for consumers and advertisers
- Multi-city tenancy via `city_slug` column (reuses go-panel/tenant Scope/ScopeClause)
- Multi-region deploy: each region (RU/US) is a separate go-grad instance + DB + Redis (242-FZ residency)
- Pluggable provider framework: magic-link now; VK ID / Yandex ID (RU) and Google / Apple (US) later
- Session entirely in Redis; Astro SSR reads via HGETALL, never HTTP-calling go-grad on the render path (F1 invariant)
- Device-link: anon epid → authenticated user merge (favorites already keyed by epid, one row insert)

Non-goals (deferred):
- SMS / OTP login (no SMS provider connected)
- VK-first login (Phase 2)
- Advertiser cabinet UI (NG2 — schema is forward-compatible but the cabinet is not built yet)
- Moscow city (Phase 3), US region (Phase 4)

## Two Isolated Auth Worlds

| Dimension | Operator (existing) | Public (this doc) |
|---|---|---|
| Package | `go-panel/auth` | `go-panel/identity` |
| Cookie name | (HMACAuth internal) | `pn_pub` |
| Cookie scope | `Path=/admin`, host-only | exact-host (`piter.now`), no `Domain=` wildcard |
| Session store | stateless HMAC token | opaque Redis hash |
| DB schema | none (stateless) | `auth.*` in per-region PG |
| Code sharing | zero | zero |

A bug in public auth cannot escalate to operator privilege because they share zero code paths, zero cookie jars, and zero stores. `admin.piter.now` is a real operator subdomain; a `Domain=.piter.now` wildcard would leak `pn_pub` into the admin host — hence exact-host only.

## Package Layout (go-panel/identity)

```
go-panel/identity/
  provider.go          # Provider interface + Registry
  store.go             # UserStore interface
  session.go           # SessionStore interface + RedisSessionStore
  auth.go              # PublicAuthenticator (wires provider + store + session)
  handlers.go          # HTTP handler builders: MagicStart, MagicVerify, Logout, LinkDevice
  email.go             # EmailSender interface + generic SMTP impl + dev log-only impl
  model.go             # User, Org, Membership, DeviceLink value types
  magiclink/
    provider.go        # MagicLinkProvider implements Provider
    token.go           # token generation, storage, single-use verify
```

The framework is pure library code. It imports nothing from go-grad. go-grad imports it and wires concrete deps (pgxpool → UserStore impl, Redis → SessionStore, SMTP creds, pepper).

## Core Interfaces

### Provider

```go
// Provider authenticates an identity claim and returns a normalized identifier.
// Each provider has a unique string name (e.g. "email", "vk", "google").
type Provider interface {
    Name() string
    // StartFlow begins the auth flow. For magic-link: sends email, stores token.
    // Returns opaque data the handler passes to the response (e.g. "check your mail").
    StartFlow(ctx context.Context, input FlowInput) (FlowStarted, error)
    // CompleteFlow verifies a callback/token and returns the provider UID.
    // provider UID is the raw string before HMAC (email address, VK user id, etc.).
    CompleteFlow(ctx context.Context, callback FlowCallback) (ProviderUID string, err error)
}

type Registry struct { /* map[string]Provider, thread-safe */ }
```

### UserStore

```go
// UserStore is implemented by go-grad/identitystore over pgxpool.
type UserStore interface {
    // UpsertIdentity finds or creates user+identity for (provider, providerUIDHmac).
    // On create: inserts user row, identity row. On find: returns existing user.
    // city_slug is carried from the session context.
    UpsertIdentity(ctx context.Context, provider string, providerUIDHmac []byte, citySlug string) (*User, error)
    // FindUser returns user by UUID. Used for session rehydration.
    FindUser(ctx context.Context, userID uuid.UUID) (*User, error)
    // ListOrgs returns org memberships for a user.
    ListOrgs(ctx context.Context, userID uuid.UUID) ([]OrgMembership, error)
    // LinkDevice records epid → user mapping (favorites merge).
    LinkDevice(ctx context.Context, epid string, userID uuid.UUID) error
}
```

### SessionStore

```go
type Session struct {
    ID          string    // opaque 256-bit hex id (crypto/rand)
    UserID      uuid.UUID
    DisplayName string
    CitySlug    string
    Orgs        []OrgMembership // snapshot, refreshed on rev bump
    Rev         int
    ExpiresAt   time.Time
}

type SessionStore interface {
    Create(ctx context.Context, s Session) error          // SET pn_sess:<id> hash, TTL
    Get(ctx context.Context, id string) (*Session, error) // HGETALL
    Revoke(ctx context.Context, id string) error          // DEL + remove from user set
    RevokeAll(ctx context.Context, userID uuid.UUID) error // logout-everywhere
    Rotate(ctx context.Context, oldID string) (newID string, err error) // fixation defense
}
```

Redis key layout:
- `pn_sess:<id>` — hash with session fields, TTL = exp
- `pn_user_sessions:<user_id>` — set of active session ids (for RevokeAll)

### EmailSender

```go
type EmailSender interface {
    SendMagicLink(ctx context.Context, to, link string) error
}
// GenericSMTPSender: reads SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM from env.
// DevLogSender: logs the link to stdout/slog, no network call. Active when SMTP_HOST="".
```

## Magic-Link Flow

```
User                   go-grad (/auth/magic/start)      Redis            Email
 │                           │                             │               │
 │──POST email──────────────>│                             │               │
 │                           │── rate-limit check ────────>│               │
 │                           │   (per-email + per-IP)      │               │
 │                           │── generate token (32B rand) │               │
 │                           │── SET pn_ml:<token_hash> ──>│ TTL=15min     │
 │                           │      {email, city_slug}     │               │
 │                           │── EmailSender.Send ─────────────────────────>
 │<── 200 "check your mail" ─│                             │               │
 │                                                         │               │
 │<────── email arrives with /auth/magic/verify?token=... ─────────────────│
 │                                                                         │
 │──GET /auth/magic/verify?token=...──────────────────────>│               │
 │                           │── GETDEL pn_ml:<hash(token)>│ (single-use)  │
 │                           │── constant-time compare     │               │
 │                           │── UpsertIdentity(provider_uid_hmac)         │
 │                           │── FindOrCreateUser                          │
 │                           │── Session.Create ───────────>│               │
 │                           │── Set-Cookie: pn_pub=<sid>  │               │
 │<── redirect to / ─────────│                             │               │
```

Token design:
- 32 bytes crypto/rand → hex-encode → that is the token in the email link
- Stored in Redis as `pn_ml:<SHA256(token)>` so the plaintext never touches Redis
- GETDEL (atomic): read + delete in one command → guaranteed single-use
- `constant-time compare` via `crypto/subtle.ConstantTimeCompare` on the SHA256

Rate limits (go-kit ratelimit):
- Per-email: 5 start attempts / 15 min
- Per-IP: 20 start attempts / 15 min

## Session Lifecycle

1. **Login**: `Rotate("") → newID`; create session hash; set `pn_pub` cookie (HttpOnly, Secure, SameSite=Lax, exact-host, no Domain=).
2. **Request**: Astro reads `pn_pub` cookie → `HGETALL pn_sess:<id>` → session struct. If exp < now or key missing → treat as logged-out. Zero HTTP calls to go-grad on normal render path.
3. **go-grad down**: Astro fails the HGETALL (Redis still up) → still renders. F1 invariant: go-grad down → public p99 unchanged.
4. **Logout**: `Revoke(id)` → DEL pn_sess:<id> + remove from user set → clear cookie.
5. **Logout-everywhere**: `RevokeAll(userID)` → scan pn_user_sessions:<uid> set → DEL all sessions.
6. **Membership change**: bump user.rev in PG → next session create (or explicit refresh) writes updated Orgs snapshot. The `rev` field lets consumers detect stale snapshots.

## Cookie Spec

```
Set-Cookie: pn_pub=<256-bit-hex>; HttpOnly; Secure; SameSite=Lax; Path=/
```

No `Domain=` attribute → exact-host binding to `piter.now`. Admin subdomain (`admin.piter.now`) never receives this cookie.

## HTTP Handler Builders

`PublicAuthenticator.MagicStartHandler() http.Handler` — rate-limited, calls provider.StartFlow, returns 200.
`PublicAuthenticator.MagicVerifyHandler() http.Handler` — calls provider.CompleteFlow, upserts user, creates session, sets cookie, redirects.
`PublicAuthenticator.LogoutHandler() http.Handler` — revokes session, clears cookie.
`PublicAuthenticator.LinkDeviceHandler() http.Handler` — reads epid from request, calls store.LinkDevice, returns 204.

go-grad mounts these on its public mux at `/auth/magic/start`, `/auth/magic/verify`, `/auth/logout`, `/auth/device/link`.

## Data Model (auth schema — lives in go-grad/identitystore)

See `go-grad/docs/identity-auth-storage.md` for the full migration SQL. Summary:

| Table | Key columns | Notes |
|---|---|---|
| `auth.users` | id uuid pk, email citext unique, display_name, city_slug, rev int default 0, created_at | city_slug = go-panel/tenant Scope |
| `auth.user_passwords` | user_id pk, hash text | US/Phase-4, argon2id; own table prevents hash leak on users SELECT |
| `auth.identities` | id, user_id, provider text, provider_uid_hmac bytea, unique(provider, provider_uid_hmac) | provider_uid stored HMAC-SHA256(value, pepper) — see ADR-002 |
| `auth.sessions_audit` | sid_hash bytea, user_id, event, ip, provider, created_at | live session in Redis; this is slow audit trail only |
| `auth.orgs` | id, name, city_slug | forward-compat for advertiser cabinet (NG2) |
| `auth.memberships` | user_id, org_id, role; pk(user_id, org_id) | empty now; no later migration needed |
| `auth.device_links` | epid text pk, user_id, linked_at | anon epid → user merge; no data migration (favorites already keyed by epid) |

No FK from `auth.*` to Directus content tables — cross-ownership is prohibited.

## Region × City Orthogonality

| Axis | What it is | How implemented |
|---|---|---|
| Region (RU / US) | Deploy boundary | Separate go-grad instance + PG + Redis per region. No `region` column. |
| City (spb / msk / sf / la) | Runtime tenant | `city_slug` column; TenantStore row. go-panel/tenant ScopeClause filters queries. |

A new city = one TenantStore INSERT (reversible). A new region = new infra instance + provider wiring (ONE_WAY for residency). See ADR-001.

The framework MUST NOT assume a single region: no package-level pepper, no package-level region string. go-grad injects these via constructor.

## Security Properties

| Property | Mechanism |
|---|---|
| provider_uid privacy (152-FZ) | HMAC-SHA256(value, per-region pepper from env) — never stored plaintext. See ADR-002. |
| Token single-use | Redis GETDEL (atomic); token in email is plaintext, only SHA256 in Redis |
| Session fixation | Rotate(oldID) on login creates new session id, deletes old |
| Cookie isolation | Exact-host, no Domain= wildcard |
| Magic-link rate limit | Per-email 5/15min + per-IP 20/15min via go-kit ratelimit |
| Audit trail | sessions_audit table: every login/logout/revoke event with sid_hash, user_id, ip, provider |
| Constant-time compare | crypto/subtle.ConstantTimeCompare on token hash verify |
| RED metrics | auth_magic_start_total, auth_magic_verify_total{result="ok|invalid|expired"}, auth_session_revoke_total |

## CI Fitness Functions

Three automated checks added to piter-now CI (Phase 1, Task 3):

1. **F1 — No Astro→go-grad HTTP on render path**: grep `src/pages/` and `src/layouts/` for fetch/axios/http calls to go-grad host. Login routes excepted. Protects F1: go-grad down → public p99 unchanged.
2. **Per-instance DSN containment (242-FZ)**: in go-grad CI, assert RU instance config references only RU-region DSN hostnames; US only US. No cross-region DSN allowed.
3. **Cookie no-wildcard**: grep HTTP handler source for `Domain=` in Set-Cookie. Any match fails CI. Protects operator/public boundary.

## Deferred Work

| Item | Phase |
|---|---|
| VK ID / Yandex ID providers | 2 |
| Moscow city (msk city_slug + TenantStore row) | 3 |
| US region (separate infra + Google/Apple providers) | 4 |
| Advertiser cabinet UI | NG2 |
| SMS / OTP | not planned |
| Password login (argon2id) | US/Phase-4 (user_passwords table already in schema) |

## ADR Index

- [ADR-001: Region as Deploy Boundary](adr/ADR-001-region-deploy-boundary.md)
- [ADR-002: Provider UID Hashing](adr/ADR-002-provider-uid-hashing.md)
