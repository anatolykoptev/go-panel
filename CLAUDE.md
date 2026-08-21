# go-panel — Repo Rules

Reusable Go admin-panel framework: declarative resources → list + detail + edit +
sidebar + auth + tenancy, rendered with templ + htmx. Consumed by go-grad,
go-job, oxpulse-admin. Module `github.com/anatolykoptev/go-panel`, Go 1.26.

**Read [DESIGN.md](DESIGN.md) before touching anything that renders.** It is the
design system and it is derived from this code, not aspirational.

## The rule this repo exists to enforce

**A list of rows is a `resource.Resource`. Nobody hand-writes a table.**

The whole value of go-panel is that one declaration buys sorting, filter chips,
pagination, cross-resource links, badges, role gating, tenant scoping, CSRF, the
Trash, and a single stylesheet — and that every screen across every consumer
looks and behaves the same. A `<table>` built with `fmt.Fprintf` in a consumer
repo throws all of that away and starts a second design system that nobody
maintains.

So when a consumer needs something the framework cannot express:

> **Add the primitive HERE. Do not work around it downstream.**

The fix then lands on every panel at once, and the next person hits a feature
instead of a wall. A downstream workaround is invisible to this repo and is
discovered later as an inconsistency — which is exactly how it goes wrong.
Measured 2026-08-21 on go-grad: a report page needed a period selector that
`admintable.FilterSpec` could not express (it only emits `WHERE`, and the grain
changes `GROUP BY`). The page was hand-rolled downstream instead. It shipped with
two undefined CSS variables, an unstyled `tfoot`, and a re-invented pill that
duplicated `.filter-chip` — none of which could happen inside the framework.

`Lister` is arbitrary SQL. "My rows are computed, not stored" is **not** a reason
to leave the framework.

## Architecture

- `resource/` — `New`/`Register`, list + detail + edit handlers, `MountPage` for
  non-list pages, `Relations` cross-links, the Trash.
- `shell/` — layout, sidebar, `styles.templ` (all CSS, inlined), nav groups.
- `components/` — `Grid`, `StatCardView`. Small on purpose.
- `auth/` — `HMACAuth` (single operator) and `BcryptTOTPAuth` + `PgxAccountStore`
  (multi-user, `panel_accounts`).
- `tenant/` — city-slug scoping, resolved INSIDE `Handler()` since v0.19.0.
- `csrf/`, `locale/`, `identity/`, `semantic/`, `render/`.
- ADRs in `docs/adr/`. Read ADR-003 before changing the framework core.

`go-panel` does NOT depend on `go-kit`. Keep it that way — consumers wire the
server themselves.

## Hard rules

1. **No CDN, no external font or script host.** All CSS/JS is embedded and
   served from the panel. Deployments sit behind ТСПУ; an external fetch is a
   dead page, not a slow one.
2. **No inline event handlers.** The admin CSP drops them. Interaction is htmx
   attributes on real `<button>`/`<a>` elements.
3. **Never build SQL from a raw request inside a `Lister`.** `ListQuery` carries
   `WhereConds`/`WhereArgs` (author-declared `SQLExpr` + literal ops only),
   `Tenant`, `Sort`, paging. Anything else the Lister needs must arrive as a
   declared, framework-validated field — add one rather than reading the request.
4. **`Spec.SQLExpr` and `Filter.SQLExpr` are author constants.** Never derived
   from user input. `Register` panics at startup on an invalid Spec/FilterSpec —
   that panic is a feature.
5. **A closed enum may become markup; an open string may not.** `switch` over
   known values and emit fixed literals; anything else goes through
   `html.EscapeString` as plain text.
6. **Only use CSS custom properties that exist.** An undefined `var()` drops the
   declaration silently. `DESIGN.md` lists the real set and the ones people
   invent by mistake.
7. **Backwards compatibility is a public contract.** Consumers pin versions;
   a renamed exported symbol is a breaking change and needs the major/minor bump
   release-please derives from the commit type.

## Build / verify

```bash
make check       # build + vet + test -race + golangci-lint + govulncheck
templ generate   # after editing any .templ — commit the generated _templ.go
```

`make check` is the gate; CI runs the same target. `shell/testdata/chrome_zero_golden.html`
is a golden file — a chrome change must update it in the same commit, and the
diff is the review artifact for anything visual.

## Releases

release-please, conventional commits, `bump-patch-for-minor-pre-major: true`
(below 1.0.0 a `feat:` bumps PATCH). Never `git tag` by hand — it desyncs the
manifest. A consumer upgrade is a separate PR in the consumer repo.

## Lockstep development with a consumer

`replace github.com/anatolykoptev/go-panel => ../go-panel` in the consumer's
go.mod while iterating. Remove it before the consumer's PR merges — a `replace`
that reaches main breaks every other consumer's build.
