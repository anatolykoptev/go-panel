# DESIGN.md — go-panel

The design system every admin built on go-panel inherits. Written from the code,
not from intent: every token, class and component named here was read out of
`shell/styles.templ`, `resource/list.templ` and `components/` at v0.23.5.

Consumers (go-grad, go-job, oxpulse-admin) do not get to invent a second system.
If a page needs something this document does not name, the change belongs
**upstream in go-panel**, not in a hand-rolled block downstream. That rule is the
whole point of the file — see [The reuse rule](#the-reuse-rule).

## Register

**Product, not brand.** These are internal operator tools: an admin looking at
rows of live data, several times a day, usually alongside a terminal. Design
serves the data. Nothing here is marketing surface.

The scene that sets the theme: *one operator, at a desk, mid-session, scanning a
table for the row that is wrong.* That forces a dark, low-glare surface with high
text contrast and colour reserved for meaning — which is what the palette below
already is. It is not dark because tools look cool dark.

## Tokens

All CSS is inlined by `shell/styles.templ` and served with the page; `pm7.css` is
linked separately at `{basePath}/static/pm7.css` for the `pm7-*` component
classes. No CDN, no external font host — deployments behind ТСПУ must work.

### Surfaces (dark, four steps)

| Token | Value | Use |
|---|---|---|
| `--bg-deep` | `#060b18` | page background, sidebar, table head |
| `--bg-base` | `#0c1222` | main content column |
| `--bg-surface` | `#131c31` | table body, cards |
| `--bg-elevated` | `#1a2540` | badges, raised chips |
| `--border` | `#1e2d4a` | table and card borders |
| `--border-subtle` | `#162038` | row separators, sidebar edge |

### Text (four steps, never pure white)

| Token | Value | Use |
|---|---|---|
| `--text-primary` | `#e8edf5` | body copy, cell values |
| `--text-secondary` | `#7b8ba8` | supporting values, chip labels |
| `--text-muted` | `#4a5b78` | table headers, captions, disabled |
| `--text-label` | `#5d6f90` | sidebar group labels |

### Meaning colours

`--accent` `#3b82f6` (links, active state, focus) · `--green` `#10b981` ·
`--yellow` `#f59e0b` · `--red` `#ef4444` · `--purple` `#a78bfa` · `--teal`
`#0ea5e9` · `--orange` `#f97316`. Each has a `-dim` companion for backgrounds
(`--red-dim`, `--green-dim`, …). Colour carries meaning here; it is never
decoration.

### Type and shape

`--font-sans` system stack · `--font-mono` (`ui-monospace, SF Mono, JetBrains
Mono, Menlo`) for anything the operator reads as data: table headers, badges,
sidebar labels, ids. Base size `.875rem`, line-height `1.5`. Radius `--radius`
`.5rem`, `--radius-lg` `.75rem`.

### Tokens that DO NOT exist

There is no `--text`, no `--bg`, no `--fg`, no `--surface`. A `var()` naming one
is invalid at computed-value time, so the whole declaration is dropped and the
element silently falls back to inherited styling — no console error, no visual
error either if the fallback happens to look plausible. **Measured 2026-08-21:**
a period selector written with `background:var(--text);color:var(--bg)` rendered
its selected chip identical to the unselected ones.

Before using a token, confirm it: `grep -oE '\-\-[a-z0-9-]+:' shell/styles.templ | sort -u`.

## Components

### Tables — `.crm-table`

The one table style. Separate borders, `--bg-surface` body over a `--bg-deep`
head, monospace uppercase headers at `.625rem`, `1px` `--border-subtle` row
separators, accent-tinted hover, last row borderless.

**`tfoot` is not styled.** A totals row written as `<tfoot><th>` inherits nothing
— `.crm-table thead th` is scoped to `thead`. Put totals in a `tbody` row with
its own class, or add the `tfoot` rules here first.

### Filter chips — `.filter-chip` / `.filter-chip.active`

Pill, `9999px` radius, transparent background, `--text-secondary`. `.active`
fills with `--accent` and white text. This is the selector affordance: period
pickers, status filters, mode switches. Do not hand-roll a pill.

Rendered by `filterBar` from two sources, both declarative:

- **`Filter.Filters` with an `Allowed` set** — one chip per allowed value.
- **`Resource.Views`** — one chip per named mode, delivered to the `Lister` as
  `ListQuery.View`. Use it for a mode a `WHERE` clause cannot express; the
  motivating case is an aggregate whose `GROUP BY` grain the operator picks.

The selected chip carries `.active`. That was dead CSS until 2026-08-21 — the
class existed from the start and nothing applied it, so no admin list had ever
shown which filter was on.

### Badges — `.badge` + `.badge-blue` / `-green` / `-red` / `-gray`

Monospace `.6875rem`, tinted `-dim` background. For closed enums only — a badge
built from an open string is how markup gets injected. Build the markup from a
`switch` over known values, never by interpolating the value.

### Cards — `.pm7-card` / `.pm7-card-content`

From `pm7.css`, not from `styles.templ`. Use for a bounded block on a
non-list page. Nested cards are always wrong. Most content does not need one.

### Stat cards — `components.StatCardView` + `components.Grid`

`components.StatCard{Label, Value, Spark []int}` rendered through
`components.Grid(...)`. The `Spark` field draws a sparkline. Do not write a
number-plus-label block by hand; this is it.

### Detail views — `.detail-page` / `.detail-section` / `.detail-items`

Rendered by the framework from `Resource.Detailer` / `FetchRow`. See
`docs/adr/ADR-004-detailer-show-view.md`.

## Interaction

**htmx, no inline handlers.** The admin CSP drops inline event handlers, so
`onclick` is dead on arrival. Filters use `hx-get` + `hx-target` + `hx-push-url`
on a `<form>`; a selector is a `<button type="submit" name=… value=…>`, which
keeps the page bookmarkable and working without scripting.

**CSRF** is `csrf.FormField = "_csrf"` on every mutating POST.

## The reuse rule

**A list of rows is a `resource.Resource`. Always.**

Declaring one gets you, for free and consistently with every other screen:
sortable headers, filter chips, pagination, cross-links between resources
(`Relations`), sidebar entry with an optional live badge, role gating, tenant
scoping, CSRF, the Trash integration, and this stylesheet.

```go
resource.Register(panel, resource.Resource{
    Name: "revenue", Title: "Revenue", Icon: "💰", Group: "Money",
    Sort:   admintable.Spec{Columns: []admintable.Column{...}},
    Filter: admintable.FilterSpec{Filters: []admintable.Filter{...}},
    Lister: func(ctx context.Context, q resource.ListQuery) ([]resource.Row, int, error) { ... },
})
```

`Lister` returns whatever rows you compute. It is **arbitrary SQL** — an
aggregate with a `GROUP BY` is as valid a resource as a plain table. "My rows are
computed, not stored" is not a reason to leave the framework.

### What NOT to do

Writing `<table>`, `<tr>` or a pill `<a>` into a `strings.Builder` in a consumer
repo. If you find yourself doing it, one of these is true:

1. **It IS a list.** Declare a Resource. This is the common case.
2. **The framework is missing a primitive.** Add it here — the fix then lands on
   every panel at once. Do not work around it downstream; a workaround is a
   second design system that nobody maintains. `Resource.Views` exists because
   this rule was broken once and then honoured: a report needed a period
   selector, `FilterSpec` could only emit `WHERE`, and the page was hand-built
   downstream before being brought back in.
3. **It genuinely is not a list** (a dashboard, a preview, a wizard). Then mount
   a page with `Panel.MountPage`, build it from the components above, and use
   only tokens that exist.

Case 2 is the one that gets skipped under time pressure. It is also the only one
that compounds.

## Copy

Sentence case for labels. No restated headings. Say what a number is when it is
not obvious — an estimate must say it is an estimate, on the page, next to the
number.

## Accessibility

Contrast is already met by the token pairs above; a new pair needs checking.
Every interactive element must be a real `<button>` or `<a>` — the filter chip is
a `<button>` for exactly this reason. Wide content scrolls inside its own
container: `.main` sets `overflow-x:auto`, the page body must never scroll
sideways.
