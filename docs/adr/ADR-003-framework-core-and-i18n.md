# ADR-003 — go-panel as the durable framework core; i18n in the resource framework; off-Directus

- Status: **Accepted** (2026-06-17)
- Deciders: operator
- Relates: ADR-001 (region = deploy boundary), ADR-002 (provider-uid hashing); go-grad ADR-0001 (tenancy = one deploy per project×country); the consent-data-layer move off Directus.

## Context

The goal is not "ship hully.day" — it is to build a **scalable framework** to spin up
place-guide sites ("project × country × locales") from **config + content**, where
**hully.day (California, US, EN+ES) is the first instance / forcing function**, not a
one-off fork. Two operator constraints sharpen the architecture:

1. **Directus is transitional — we are migrating off it.** Consent already moved to
   go-grad; content (places/collections) follows. So nothing durable should be anchored
   in Directus-specific features (e.g. Directus translations) — that work dies with it.
2. **i18n is a framework capability, not a per-fork hack.** hully needs EN+ES;
   piter-now is single-locale (RU). Future projects will have their own locale sets.

go-panel is already a full **declarative admin/CRUD framework** (packages: `identity`,
`auth`, `csrf`, `render`, `resource`, `shell`, `tenant`). Its `resource.Resource` is a
declarative contract (Name/Title/Sort/Filter/Scope/Lister/Writer) and its core principle
is **"the app owns the schema; go-panel never assumes one"** — `Lister`/`Writer`
closures own data access; go-panel provides the UI + contract. It is already
tenant-aware (`tenant.Scope` on `city_slug`).

## Decision

**go-panel is the durable framework core. Content lives in the app's store (go-grad) as
go-panel resources. Directus is a transitional editor being retired. i18n is added to
go-panel's resource framework. hully is the first go-panel-native instance.**

### Framework layers (per-project = config + content; core = reusable)

| Layer | Core (framework) | Per-project (config) | Status |
|---|---|---|---|
| go-panel | identity + auth + csrf + render + **resource/admin CRUD** + **tenant** + **i18n (new)** | which resources, locales, theme | resource/tenant ✅; i18n ⬜ |
| go-grad (app on go-panel) | the service + Lister/Writer + store | env: jurisdiction/basis/policy/DB | basis config ✅ (#36); content resources ⬜ |
| Astro frontend | reusable core: components, data-layer, **i18n routing** | brand / locales / domains / API base | ⬜ (biggest gap) |
| Provisioning | compose + bootstrap template | project / country / secrets | 🟡 (hand-rolled per deployment) |

### i18n design (where it lives)

Consistent with "app owns schema", i18n splits cleanly:

- **go-panel (framework) owns the i18n CONTRACT + admin UI:** a `Locale` concept
  (project's configured locales + default), a **translatable** marker on form fields,
  and the admin **per-locale editing UI** (locale tabs/switcher in `form.templ`, a
  locale-aware list view). go-panel does NOT store translations.
- **The app (go-grad) owns the STORAGE + serving:** its `Writer` persists per-locale
  values (a `<entity>_translations` table or a JSONB locale-map — app's choice) and its
  `Lister`/API serves locale-aware rows. go-panel hands the app the active locale; the
  app's closure does the rest. This keeps go-panel schema-agnostic.
- **Astro frontend** consumes the locale-aware go-grad API and adds **Astro i18n
  routing** (`/` = EN default, `/es/` = ES) + UI-string translation. go-grad/go-panel
  stay locale-data sources; presentation routing is the frontend's.

Locale is an axis **orthogonal to tenant** (`tenant.Scope` = which rows; `Locale` =
which language of a row's translatable fields). They compose.

### hully = first go-panel-native instance

hully's Place/Collection are **go-panel resources backed by go-grad's own store**
(not Directus), translatable (EN+ES), edited via the go-panel admin, served locale-aware.
hully therefore has **no Directus dependency for content** — proving the durable
framework and the off-Directus direction with zero future migration (it starts native).

## Consequences

- **Positive:** i18n is durable (survives Directus), reusable (any go-panel resource gets
  it), and its delivery doubles as a step in retiring Directus. hully validates the whole
  framework, not just a fork.
- **Cost:** building i18n into the resource framework + giving hully a native content
  store is more Go work now than "Directus + Astro i18n". Accepted — it is the
  framework-correct, throwaway-free path, and hully is greenfield (native from day 1).
- **Pillow Directus (already stood up) is now likely unnecessary for hully content** —
  drop it, or keep only as a transient WP-import staging; keep Postgres + Redis.
- **Discipline (no premature abstraction):** generalise as the second instance forces it
  (extract-don't-predict / rule of three) — exactly how go-grad #36 was born (hully/US
  exposed the basis hardcode → config). Build the i18n primitives hully needs, not a
  giant i18n system; lay the seams now so hully is instance #1, not a fork-to-refactor.

## Implementation phases (this ADR's deliverables)

1. **go-panel i18n core**: `Locale` config + a translatable field marker + per-locale
   admin form (locale tabs) + locale handoff to `Lister`/`Writer`. Tests.
2. **go-grad content resources for hully**: Place/Collection resources backed by go-grad's
   store, translatable, with a per-locale storage shape.
3. **Astro frontend foundation**: reusable core + per-project config + Astro i18n routing,
   consuming go-grad's locale-aware API. hully = instance #1.
4. **Retire Directus for hully** (and chart piter's eventual move).

## Open questions
1. Per-locale storage shape in go-grad — `<entity>_translations` table (relational,
   queryable) vs JSONB locale-map (simpler, denormalised)? Lean relational for SEO/query.
2. Locale negotiation default — path-prefix only (`/es/`) vs + `Accept-Language`? Lean
   path-prefix (explicit, SEO-clean), no auto-redirect.
