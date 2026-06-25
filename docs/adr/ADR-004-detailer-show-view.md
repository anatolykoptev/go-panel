# ADR-004 — Reusable Detail (Show) view primitive in go-panel

- Status: **Accepted** (2026-06-25)
- Deciders: operator
- Relates: ADR-003 (go-panel as durable framework core); architecture plan `plans/go-job/2026-06-25-adminui-fit-display-architecture.md`

## Context

go-panel's `resource.Resource` provides list, filter, sort, create, and edit pages.
The one universal admin capability it lacked was a **per-record detail (Show) page** —
a full-page view at `GET {basePath}/{name}/{id}` where an operator can see the
complete record without an edit form.

The immediate driver is the go-job admin surface: surfacing per-job fit-card data
(Why / Gaps / Market Read / Success reasoning) requires more screen real estate than
a list cell permits.  The pattern is universal: go-nerv entity views, go-job fit cards,
bounty/freelance detail cards — every admin eventually needs it.

ADR-003 ("go-panel as the durable framework core") explicitly sanctions adding reusable
capabilities to go-panel rather than per-admin hacks.  Every mature server-rendered
admin framework (Django, Rails Administrate, Laravel Nova) ships a per-record detail
page.  This ADR fills that universal gap.

## Decision

Add `Detailer func(ctx context.Context, id string) ([]DetailSection, error)` to
`resource.Resource` as an **opt-in, nil-default** field.

- **Nil = no change**: resources without a Detailer continue to work exactly as today.
  No existing consumer breaks.
- **Non-nil = detail page mounted**: `Register` mounts `GET {basePath}/{name}/{id}`.
  id=="new" returns 404 (symmetric with the edit route).  The handler calls the closure,
  wraps the result in `shell.Layout`, and renders `detail.templ`.
- **Schema-agnostic shape**: the closure returns `[]DetailSection`, each with an
  optional title, a list of `DetailItem` (label + value + HTML flag), or a `RawHTML`
  block for consumer-built panels.  go-panel owns the chrome; the consumer owns the
  content.  This matches ADR-003's principle: "the app owns the schema; go-panel
  never assumes one."
- **XSS contract**: `DetailItem.Value` is HTML-escaped by go-panel (via templ auto-escape)
  unless `HTML=true`.  `HTML=true` is for closed-enum chip HTML assembled by the consumer
  from constant inputs — never for raw DB or user text.  `DetailSection.RawHTML` is
  rendered via `templ.Raw`; the consumer guarantees it is XSS-free.

## Status-chip CSS (bundled in this PR)

The same PR adds two reusable CSS chip families to `shell/styles.templ`:

- `.fit-chip` + `.fit-strong / .fit-moderate / .fit-weak / .fit-low / .fit-reject / .fit-unscored`
  — fit axis (green→red ramp, monospace, namespaced `fit-*`).
- `.suc-chip` + `.suc-strong / .suc-moderate / .suc-longshot` — market-read axis
  (purple diamond family, deliberately orthogonal to fit colors).
- `.ou-glyph` + `.ou-over / .ou-match / .ou-under` — over/under qualification glyphs.
- Detail-page layout classes: `.detail-section`, `.detail-items`, `.detail-item`,
  `.fit-card`, `.market-card`, and related sub-classes for consumer-built HTML panels.

These are namespaced to avoid collision with existing `.badge-*` classes (semantic
green/red for system success/error — orthogonal meaning).

## Column Width / Align wiring (bundled)

`admintable.Column.Width` and `.Align` existed but were silently ignored by `list.templ`.
This PR wires them to `style="width:…;text-align:…"` on `<th>` and `<td>` elements.
The change is a two-function addition in `list.templ`; existing resources without
Width/Align set emit no style attribute (zero behaviour change).

## Consequences

**Forward-only API surface**: `Detailer` is a new public field on `resource.Resource`.
Removing it after consumers adopt it is a breaking library change + a release.  The data
path is reversible (read-only); the API surface is not.  Hence the ADR ceremony.

**Positive**: every future resource in go-job, go-nerv, bounties, freelance, and any
other go-panel-backed admin gains an opt-in detail page via one closure — no per-admin
layout duplication.  `Row.Href` can now point to the in-admin detail page instead of an
off-site URL (better UX; external URL moves into the detail body as a link).

**Trade-offs**: +1 public field, +1 route per resource with Detailer set, +1 template,
+1 ADR, +1 release tag.  Cost is proportional to adoption; zero cost for resources that
don't opt in.

## Alternatives considered

1. **Htmx expandable row** (no new route; `hx-get` injects a `<tr>` below the clicked
   row). Cheaper, no API change, no release.  Rejected as primary because a URL-addressable
   page is more linkable / bookmarkable and matches the Django/Rails precedent.  Remains
   a valid alternative for operators who want to avoid a go-panel release.
2. **go-job-local custom handler** mounted alongside the panel.  Rejected — bypasses
   framework auth/nav/layout, does not generalize, violates ADR-003.
3. **Structured `Cell.Chip *ChipSpec`** field rendered by the template.  More type-safe
   than `Cell.HTML`, no raw HTML in the list.  Deferred — `Cell.HTML` is the
   subtraction-first choice (ADR-003 A1) that ships without a framework release.
   A typed chip field is a worthwhile future enhancement.
