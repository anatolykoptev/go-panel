# DESIGN.md — go-panel

The design system every admin built on go-panel inherits. Written from the code,
not from intent: every token, class and component named here was read out of
`shell/styles.templ`, `resource/list.templ` and `components/`.

Consumers (go-grad, go-job, oxpulse-admin) do not get to invent a second system.
If a page needs something this document does not name, the change belongs
**upstream in go-panel**, not in a hand-rolled block downstream. That rule is the
whole point of the file — see [The reuse rule](#the-reuse-rule).

## Register

**Product, not brand.** These are internal operator tools: an admin looking at
rows of live data, several times a day, usually alongside a terminal. Design
serves the data. Nothing here is marketing surface.

**Canon.** go-panel ships the fleet admin canon v2.1 (go-nerv `DESIGN.md`): Linear-class
structure on VS Code graphite, one restrained blue accent, surgical 1px rules,
air over chrome, colour reserved for status and interaction. Two themes share one
vocabulary: **graphite dark** (canon v2.1 verbatim, the default) and **graphite
light** (VS Code Light Modern family: hue-flat neutrals, the same single blue,
the same six status roles). The theme is the `sb-t` cookie, not the OS; a
missing or unrecognised cookie renders dark. Where a page and this file
disagree, the page is wrong.

## Tokens

All CSS is inlined by `shell/styles.templ` and served with the page; `pm7.css` is
linked separately at `{basePath}/static/pm7.css` for the `pm7-*` component
classes. No CDN, no external font host — deployments behind ТСПУ must work.
Bare `:root` is the light palette, `:root.dark` the dark one; a token declared
once as an alias (`--text-muted:var(--text-secondary)`) resolves per theme.

### Neutrals (hue-flat, never `#000` as a ground)

| Token | Dark | Light | Use |
|---|---|---|---|
| `--bg-deep` | `#141414` | `#f3f3f3` | page base, sidebar, table head, `<pre>` wells |
| `--bg-base` | `#181818` | `#f8f8f8` | main content column |
| `--bg-raised` | `#1f1f1f` | `#ffffff` | tables, the one container level |
| `--bg-elevated` | `#282828` | `#ececec` | row hover, inputs (dark), neutral badges |
| `--border` | `#2b2b2b` | `#d4d4d4` | structural 1px rules |
| `--border-subtle` | `#232323` | `#e5e5e5` | row dividers, sidebar edge |
| `--text-primary` | `#d6d6d6` | `#242424` | body, cell values |
| `--text-secondary` | `#9a9a9a` | `#595959` | labels, metadata, column heads (AA on every ground) |
| `--text-faint` | `#6a6a6a` | `#8a8a8a` | DECORATIVE ONLY (3.05 / 3.45 on raised) — never text that carries meaning |

### Accent — one blue, interaction only (≤10% of a screen)

| Token | Dark | Light | Use |
|---|---|---|---|
| `--accent` | `#0078d4` | `#005fb8` | fills (primary button, active nav), focus ring |
| `--accent-text` | `#4daafc` | `#005fb8` | links, active-state text |
| `--accent-soft` | `#04395e` | `#d6e6f7` | selected / active-chip background |

White on `--accent` is 4.53:1 (dark) / 6.31:1 (light). There is no second accent.

### Status roles (badges, notices, tones, deltas)

Solid dim background + bright text of the same hue. **No `rgba` badge or notice
backgrounds, no violet anywhere** — both are tested.

| Role | Dark text / bg | Light text / bg | Meaning |
|---|---|---|---|
| positive | `#52d99a` / `#113327` | `#0b6a3a` / `#d9f2e3` | done, active, up |
| warning | `#e8b04f` / `#332711` | `#7a4a00` / `#fbecc8` | pending, follow-up |
| negative | `#f28b82` / `#3a1d1a` | `#a3231a` / `#fbe3e0` | failed, overdue, down |
| info | `#4daafc` / `#0f2c44` | `#005fb8` / `#dbe9f8` | in flight, new |
| special | `#f0a15c` / `#38220f` | `#944000` / `#fbe5d2` | evaluation (orange; replaces purple) |
| neutral | `#9a9a9a` / `#282828` + 1px `--border` | `#595959` / `#ececec` + 1px `--border` | tags, unknown fallback |

Tokens: `--positive` `--positive-bg` … `--neutral` `--neutral-bg`.

### Legacy names (kept; resolve onto the canon)

| Legacy | Resolves to |
|---|---|
| `--bg-surface` | `--bg-raised` |
| `--text-muted`, `--text-label` | `--text-secondary` (their consumers — table heads, sidebar labels — are load-bearing and must hold AA) |
| `--accent-link`, `--accent-light` | `--accent-text` |
| `--accent-dim` | `--accent-soft` |
| `--accent-glow` | translucent accent wash (`rgba`), sidebar hover only — never a badge ground |
| `--green` / `-dim` / `-light` | `--positive` / `-bg` / `--positive` |
| `--yellow*` | warning · `--red*` negative · `--teal*` info |
| `--orange*`, `--purple*` | special (violet is retired) |
| `--badge-gray-fg` | `--neutral` |
| `--fit-*`, `--suc-*`, `--ou-*` | status roles (`suc-strong` info, `suc-moderate`/`longshot` neutral) |
| `--btn-primary-bg` | `--accent` (hover `#006abe` dark / `#00539f` light); `--btn-danger-bg` `#b91c1c` |

### Contrast (WCAG relative luminance, asserted by `resource/theme_canon_test.go`)

| Pair | Dark | Light |
|---|---|---|
| primary on deep / base / raised / elevated | 12.68 / 12.22 / 11.34 / 10.14 | 13.99 / 14.62 / 15.52 / 13.14 |
| secondary on deep / base / raised / elevated | 6.55 / 6.31 / 5.86 / 5.24 | 6.31 / 6.60 / 7.00 / 5.93 |
| accent-text on deep / base / raised / elevated | 7.43 / 7.16 / 6.65 / 5.95 | 5.68 / 5.94 / 6.31 / 5.34 |
| accent-text on accent-soft | 4.83 | 4.96 |
| white on accent / primary hover | 4.53 / 5.52 | 6.31 / 7.67 |
| positive · warning · negative · info · special · neutral on own bg | 7.70 · 7.47 · 6.41 · 5.79 · 7.09 · 5.24 | 5.66 · 6.39 · 6.10 · 5.11 · 5.76 · 5.93 |

`--text-secondary` on `--accent-soft` is 4.26 in dark: not an allowed pair (an
active chip uses `--accent-text`). A new pair is checked before it lands; the
test also derives pairs from every stylesheet rule that sets a background and a
colour.

### Type, spacing, geometry

`--font-sans` system stack · `--font-mono` (`ui-monospace, SF Mono, JetBrains
Mono, Menlo`) for instrument labels, badges, ids. **Floor: 11px, nothing smaller**
(tested). Body `.9375rem` / 1.5.

| Step | Size | Weight | Use |
|---|---|---|---|
| page title | 22px / 1.2, −.012em | 650 | `.page-header h2`, login title |
| section title | 17px / 1.35 | 600 | `.detail-section-title`, trash sections — sans, sentence case |
| body | 15px | 400 | prose, cell values, `.row-name` (600) |
| meta | 13px | 400 | `.row-sub`, `.page-header p`, secondary text |
| badge / chip | 12px | 500 | `.badge`, `.filter-chip` |
| micro | 11px mono caps, .06em | 600 | column heads, field labels, sidebar group labels |

Rhythm: 8 in a control, 16 in a group, 24 between groups, 40 between sections;
page gutter 32; content max-width 1400px. Radius **6px** controls
(`--radius`), **10px** surfaces (`--radius-lg`). Table cells `12px 16px`
(rows ≥44px). One focus treatment everywhere: 2px `--accent` ring, offset 2px.

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

The one table style. One `--bg-raised` surface (radius 10) over a `--bg-deep`
head, `micro` headers in `--text-secondary`, `1px` `--border-subtle` row
separators, `--bg-elevated` row hover and the row name turning `--accent-text`,
numbers tabular, last row borderless. Sticky headers are not implemented: the
`.crm-table-scroll` wrapper is itself a scroll container.

**`tfoot` is not styled.** A totals row written as `<tfoot><th>` inherits nothing
— `.crm-table thead th` is scoped to `thead`. Put totals in a `tbody` row with
its own class, or add the `tfoot` rules here first.

**`.crm-table--static`** is the modifier for a detail-page table whose rows do
not navigate. The base `.crm-table` sets `cursor:pointer` and an accent-tinted
row hover because list rows link somewhere; a static table drops both (cursor
default, hover transparent) so a non-interactive row does not pretend to be a
link. Rendered by `DetailSection.Table` on the detail page.

**A table inside a card does not draw a card.** `.crm-table` paints
`--bg-raised`, a `1px --border` and `--radius-lg`; so does `.pm7-card` (and
`.detail-section` did before the canon dropped its card). Nested, they put two outlines a few pixels apart on an identical
background — the inner one reads as an artefact rather than a boundary, and
only the containers holding a table get it, so they look heavier than their
neighbours for no reason a reader could name. Inside either container the table
therefore drops its background, border and radius, its outer cells lose their
side padding so the columns line up with the container's own text, and the
`thead` loses its `--bg-deep` strip (which reads as a floating bar once there
is no frame to clip it — the header cells already carry the same
mono/uppercase/muted treatment as a section label, so an underline is the whole
separation needed).

The condition is **structural**, not a modifier: it hangs off the nesting, not
off `.crm-table--static`. That modifier answers a different question ("do these
rows navigate"), and a consumer dropping a plain `.crm-table` into a
`.pm7-card` — a dashboard summary, a cabinet overview, a billing preview — has
done nothing wrong and must not inherit a doubled frame for it. CSS cannot say
"any ancestor that draws a border", so the card containers are named
explicitly; **a new card container has to be added to that selector list.**

### Filter chips — `.filter-chip` / `.filter-chip.active`

Quiet pill, `9999px` radius, 1px `--border`, transparent background,
`--text-secondary`, 12px. `.active` is `--accent-soft` + `--accent-text` + an
`--accent` border (solid-accent chips shout; retired). This is the selector affordance: period
pickers, status filters, mode switches. Do not hand-roll a pill.

Rendered by `filterBar` from two sources, both declarative:

- **`Filter.Filters` with an `Allowed` set** — one chip per allowed value.
- **`Resource.Views`** — one chip per named mode, delivered to the `Lister` as
  `ListQuery.View`. Use it for a mode a `WHERE` clause cannot express; the
  motivating case is an aggregate whose `GROUP BY` grain the operator picks.

The selected chip carries `.active`. That was dead CSS until 2026-08-21 — the
class existed from the start and nothing applied it, so no admin list had ever
shown which filter was on.

### Badges — `.badge` + `.badge-blue` / `-green` / `-red` / `-gray` / `-orange` / `-purple` / `-yellow`

Mono 12px, radius 6, padding `2px 8px`, a canon status pair (solid dim
background, bright text of the same hue). The legacy colour classes map onto
roles — blue→info, green→positive, red→negative, yellow→warning,
orange and purple→special, gray→neutral — and the role names
(`.badge-positive|-warning|-negative|-info|-special|-neutral`) are aliases.
`.tone-*` has the same aliases. For closed enums only — a badge
built from an open string is how markup gets injected. Build the markup from a
`switch` over known values, never by interpolating the value.

### Notices — `components.NoticeView` / `.notice` + `.notice-info|-warn|-danger|-muted`

The page-level message block: a warning that a total is incomplete, a caveat
that a figure is an estimate, a banner saying an action is blocked.
`components.Notice{Level, Title, Text, LinkHref, LinkText}`.

- Renders **nothing** when `Text` is empty, so a caller computing a conditional
  message needs no guard of its own and can never leave an empty coloured box.
- `NoticeDanger` and `NoticeWarn` carry `role="alert"`; `NoticeInfo` and
  `NoticeMuted` carry `role="note"`, so a footnote does not interrupt a screen
  reader.
- `LinkHref` is sanitised by templ. Do not wrap a computed URL in
  `templ.SafeURL` — that asserts a safety the component cannot check.

**No side stripe.** The block this replaced was hand-written in three consumers
as a `pm7-card` with `border-left:3px solid var(--red)`. A coloured left border
carries nothing to a screen reader and reads as an accident at narrow widths; a
full 1px border (the status pair mixed 40/60) over the solid status background
states the same severity with the whole element. No `rgba`.

### Cards — `.pm7-card` / `.pm7-card-content`

From `pm7.css`, not from `styles.templ`. Use for a bounded block on a
non-list page. Nested cards are always wrong. Most content does not need one
(canon: air and 1px rules before a box).

### Stat cards — `components.StatCardView` + `components.Grid`

`components.StatCard{Label, Value, Spark []int}` rendered through
`components.Grid(...)`. The `Spark` field draws a sparkline. Do not write a
number-plus-label block by hand; this is it.

### Buttons — `.btn` + `-primary` / `-secondary` / `-ghost` / `-danger`

Sizes `-sm` / `-lg`, `-block`; `disabled` / `aria-disabled="true"` dims and blocks
the pointer; `:focus-visible` draws the `--accent` ring. 36px high on desktop,
44px at ≤767px. One primary per row or region. Danger is for a confirmed
destructive step, not a decoration — a reversible delete is a ghost link.
Form controls (`.pm7-input` select/input/textarea/datetime-local) take their
fill from `--pm7-input` (`--bg-elevated` in dark, white in light) and text from
`--text-primary`, radius 6, focus ring offset 2px.

### Definition list — `.dl-compact`

`<dl class="dl-compact"><div><dt>…</dt><dd>…</dd></div>…</dl>`: mono 11px label
over a 14px value, for record metadata.

### Work-queue components — `components.QueueList` / `QueueRowView`, `FunnelView`, `StepperView`, `Timeline`, `KPIStrip`

Pure templ, auto-escaped, importing only templ + stdlib. Colour is always a
closed `components.Tone` (never a class string) and always accompanied by text.
A control that changes state is a `components.PostAction`: a real
`<form method=post>` with `hx-post` on top (works without JS, answers 303), and
the CSRF token comes from `Panel.CSRFToken(r)`. `/admin/showcase/` in
`example/` renders each of them.

### Lists on phones, emphasis, chip labels

A list `<table class="crm-table" data-stack="cards">` becomes stacked cards at
≤767px (each cell prints its `data-label` column header; the header row stays as
sort links). `Row.Emphasis` bolds a row (unread). `Resource.FilterLabels`
separates a filter chip's text from the value it submits.

### Detail views — `.detail-page` / `.detail-section` / `.detail-items`

Rendered by the framework from `Resource.Detailer` / `FetchRow`. Canon layout:
no card per section — a 17px sans section title over its content, sections 40px
apart; items are a field grid (`repeat(auto-fit,minmax(180px,1fr))`, gap 16/24)
of `micro` label over a 15px value. See
`docs/adr/ADR-004-detailer-show-view.md`. A section carries any of: a list of
label/value `DetailItem`s, a multi-column `DetailTable` (rendered as
`.crm-table--static`), or a `RawHTML` block. `RawHTML` takes precedence; otherwise
the items render above the table — a summary line above its tabular data is the
intended shape.

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

Contrast is asserted for every token pair above and for every stylesheet rule
that sets a background and a colour; a new pair needs checking.
Every interactive element must be a real `<button>` or `<a>` — the filter chip is
a `<button>` for exactly this reason. Wide content scrolls inside its own
container: `.main` sets `overflow-x:auto`, the page body must never scroll
sideways.


## Migrating from the navy palette (consumers: go-job, go-grad, oxpulse-admin)

No API changed: a zero-value consumer compiles and behaves identically. Only
visuals change when the go-panel version is bumped.

- **Grounds** go from navy (`#060b18` / `#0c1222` / `#131c31`) to hue-flat graphite
  (`#141414` / `#181818` / `#1f1f1f`); the accent from `#3b82f6` to VS Code blue
  `#0078d4` (fills) with `#4daafc` for links and active text.
- **Badges and notices** are solid status pairs now, so a badge on a mid-tone
  background no longer fails contrast. `badge-purple`, `badge-orange` and
  `tone-purple` still exist but render the orange "special" pair: violet is gone.
  If a page used purple to tell two things apart, give one of them another role.
- **Muted text is brighter.** `--text-muted` and `--text-label` now equal
  `--text-secondary` (`#9a9a9a`); table headers and labels are legible but no longer
  a dim third step. Use `--text-faint` only for decoration.
- **Bigger type, bigger rows.** Body 14→15px, nothing under 11px (labels were 9–10px),
  table rows ~44px, detail sections lose their cards and the field list becomes a grid.
- **Active filter chip** is a soft-blue chip with an accent border, not a solid blue one.
- **Hand-written colours** (`#3b82f6`, `rgba(59,130,246,…)`, `var(--purple)`, hardcoded
  badge backgrounds) in consumer templates will no longer match: replace them with
  the tokens above. A consumer that already pinned a theme cookie keeps its choice;
  light is now a coherent graphite light rather than a slate one.
- The embedded `pm7.css` is go-nerv's canon-retuned build (see the commit message):
  pm7 pages and the login screen follow the same graphite grounds.
