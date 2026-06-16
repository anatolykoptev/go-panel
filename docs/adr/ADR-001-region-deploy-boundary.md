# ADR-001: Region as Deploy Boundary

**Status:** Accepted
**Date:** 2026-06-15
**Deciders:** operator

## Context

The platform must serve users in Russia (RU) and the United States (US). Russian Federal Law 242-FZ (data localization) requires that personal data of Russian citizens be stored and processed on servers physically located in Russia. This includes any data that can identify a user — email addresses, session data, identity records.

Two design options were considered:

**Option A — Column-based multi-tenancy**: one go-grad instance, one database, one Redis. Add a `region` column to every `auth.*` table. Filter all queries by region.

**Option B — Instance-based isolation**: each region runs its own go-grad binary, its own PostgreSQL database, and its own Redis cluster — all physically in-region. No `region` column.

## Decision

**Option B — instance-based isolation.**

Each region (RU, US) is a distinct deployment unit:
- Separate go-grad binary instance
- Separate PostgreSQL cluster (physically in-region per 242-FZ)
- Separate Redis cluster (physically in-region)
- Separate environment: PG DSN, Redis URL, auth pepper, SMTP credentials, OAuth client IDs all differ per region and are injected via env — never committed to source

No `region` column exists anywhere in the `auth` schema. A RU instance has no US DSN in its environment; a US instance has no RU DSN. Cross-region data access is not a misconfigured query — it is a missing DSN, which produces a startup error.

The `go-panel/identity` framework is region-agnostic: no package-level region string, no package-level pepper. go-grad injects all region-specific configuration at construction time.

## Forcing Constraints

1. **242-FZ data localization (Russia)**: Russian user PII must reside on Russian-territory servers. A column-based design makes a cross-region data leak a single-character bug (wrong region value in a WHERE clause, or a missing WHERE clause). An instance-based design makes it a deployment-time configuration error — detectable before any user data is processed.

2. **Fail-safe property**: the preferred failure mode for a residency constraint is "does not start" rather than "silently serves wrong data". Missing DSN = startup failure = no data at risk.

3. **Per-region provider variation**: RU will use Yandex ID / VK ID (Phase 2); US will use Google / Apple. These provider credentials differ and should not be co-mingled in a single config file.

4. **Per-region pepper**: auth.identities stores HMAC(provider_uid, pepper). The pepper must differ per region so that a DB dump from one region cannot be used to brute-force identities in another.

## Consequences

**Positive:**
- Residency is structural, not procedural. No WHERE-clause audits required.
- Per-region failure isolation: a RU DB outage does not affect US users and vice versa.
- Provider credentials and pepper are cleanly scoped.
- Framework code is simpler: no region routing logic.

**Negative:**
- N sets of infrastructure to provision, monitor, and upgrade.
- Schema migrations must be applied to each regional DB independently. Automated via the same migration runner (`auth_schema_migrations`) but executed per-deploy.
- go-grad releases must be deployed to each region separately. Acceptable: regions are independent tenants.

**Constraints on the framework:**
- `go-panel/identity` MUST NOT declare any package-level variable holding a region name, pepper, or DSN.
- go-grad MUST NOT accept a config that mixes DSNs from different regions.
- CI MUST assert per-instance DSN containment (see fitness function #2 in identity-design.md).

## Reversal

**ONE_WAY.** Merging two regional databases into one would require a cross-border data migration that 242-FZ forbids for Russian user data. This decision is irreversible once the first real Russian user creates an account. Choose instance-based isolation before first login.

## Alternatives Considered

**Option A (column-based)** was rejected because:
- A missing `WHERE region = $1` silently serves cross-region data — the error is invisible until audited.
- 242-FZ does not recognize "we filter by column" as compliant; physical location of the server is what matters.
- Single-instance design would require a single Redis serving both regions, which means session data crosses the border in memory even if the PG rows don't.

**Subdomain routing** (one instance, routing by subdomain to different DB pools) was also rejected: still a single process with access to both pools, which means a bug in the routing layer exposes both pools.
