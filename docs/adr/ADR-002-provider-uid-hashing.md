# ADR-002: Provider UID Storage as HMAC-SHA256

**Status:** Accepted
**Date:** 2026-06-15
**Deciders:** operator

## Context

The `auth.identities` table links a user account to an external identity claim. The external identifier (provider UID) is the raw value the provider authenticates: an email address for magic-link, a VK user ID for VK OAuth, a Google subject for Google OAuth.

These values are small-keyspace or guessable. An email address is ~30 bits of entropy (common addresses); a VK user ID is a sequential integer. If stored in plaintext, a DB dump exposes the complete link between accounts and external identities. If hashed with a bare SHA-256 (no key), a precomputed rainbow table for common emails or a linear scan of VK IDs would reverse every identity record.

**152-FZ (Russia)** requires minimization of personal data — storing a hashed form that cannot be reversed is preferable to storing plaintext when plaintext is not needed for any operational purpose.

## Decision

Store `provider_uid_hmac = HMAC-SHA256(provider_uid, per_region_pepper)` as `bytea` in `auth.identities`. Never store the plaintext provider UID in the `auth` schema.

`per_region_pepper` is:
- A secret byte string, at least 32 bytes, generated once per region during initial provisioning
- Stored in the region's secret store (environment variable `AUTH_IDENTITY_PEPPER`)
- Never committed to source control
- Never logged

Lookups:
```go
// To find an existing identity:
hmac := computeHMAC(providerUID, pepper) // HMAC-SHA256
row, err := db.QueryRow("SELECT user_id FROM auth.identities WHERE provider=$1 AND provider_uid_hmac=$2", provider, hmac)
```

The HMAC is computed in the application (go-grad), not in the DB. The DB stores the result.

## Forcing Constraints

1. **152-FZ minimization**: Russian personal data law requires storing only what is necessary. A keyed HMAC satisfies "cannot be reversed without the key" — a regulator auditing the DB sees opaque bytes.

2. **Rainbow table resistance**: email addresses occupy a small keyspace (billions, not 2^256). Bare SHA-256 of common emails is precomputed. HMAC-SHA256 with a 32-byte secret key makes the precomputed table useless without the key — equivalent to AES-128 keyed-hash strength.

3. **VK user ID range**: VK IDs are sequential integers < 10^9. A bare SHA-256 column can be reversed by scanning 10^9 possible values in minutes on commodity hardware. HMAC with a secret key defeats this.

## Consequences

**Positive:**
- A DB dump without the pepper yields no recoverable email addresses or provider IDs.
- Satisfies 152-FZ minimization claim for the identity table.
- Lookup is deterministic: same provider_uid + same pepper → same HMAC → O(1) index lookup.

**Negative:**
- The raw provider UID is never displayable from the DB. This is intentional and acceptable — no operational feature requires displaying the raw provider UID.
- Pepper rotation requires re-computing HMAC for every identity row and transactionally replacing them. This is a one-time migration per rotation event. The scheme is chosen once; rotation is expensive but safe.
- The pepper must be backed up with the same durability as the database. If the pepper is lost, all identity links are severed — users would need to re-authenticate (a new identity row would be created, linking their existing user via the email uniqueness constraint on `auth.users`).

**Constraints on the framework:**
- The `computeHMAC` function MUST use `crypto/hmac` with `sha256.New` (not a bare `sha256.Sum256`).
- The pepper MUST be passed as a constructor argument to `go-panel/identity.PublicAuthenticator` — never read from a global variable.
- `go-grad` MUST read the pepper from `AUTH_IDENTITY_PEPPER` env var at startup and fail fast if empty.
- The HMAC output is 32 bytes; store as `bytea(32)` with a btree index on `(provider, provider_uid_hmac)`.

## Alternatives Considered

**Plaintext storage**: rejected. 152-FZ minimization + unnecessary PII exposure in DB dumps.

**Bare SHA-256**: rejected. Rainbow tables defeat it for small-keyspace inputs (email, VK ID). Accepted only for large-entropy inputs (OAuth tokens), which don't apply here.

**Bcrypt/Argon2id**: rejected for identity lookups. These are designed to be slow (login-time acceptable). Identity lookup happens on every magic-link verify — O(1) HMAC is sufficient and does not introduce latency on the verification path.

**Encryption (AES-GCM)**: rejected. Encryption is reversible with the key; if the key leaks, all plaintext is exposed. HMAC is one-way even with key in hand — you can verify a candidate value but cannot recover the plaintext from the stored value alone.
