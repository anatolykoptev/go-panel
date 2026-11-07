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
| `--border-control` | `#2b2b2b` | `#858585` | edge of inputs, chips, secondary buttons (light reaches 3:1, WCAG 1.4.11) |
| `--track` | `#404040` | `#c8c8c8` | funnel / progress track (≥1.5:1 on every ground) |
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

Sizes `-sm` / `-lg`, `-block`; `disabled` / `aria-disabled="true"` restyles and blocks
the pointer; `:focus-visible` draws the `--accent` ring. Disabled is an explicit pair (`--bg-elevated` + `--text-secondary`), never opacity. 36px high on desktop,
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


## Skins

A skin is an opt-in re-valuing of the tokens above plus structural overrides.
**Graphite** (everything documented so far) is the default and the zero value.
**Iris** is the second skin: light-first, drawn in pen.dev for the go-grad
sales manager. Source: `shell/skin_iris.templ`, `shell/skin_fonts.go`.

### Opting in

```go
resource.New(resource.Config{
    Skin:         shell.SkinIris, // "" / unknown = graphite
    DefaultTheme: "",             // optional: shell.ThemeLight|ThemeDark; "" = the skin's default
    // ...
})
```

`Skin` rides `ChromeState` (set by `chromeStateFrom`, and by a small middleware
for the login and MFA routes the authenticators mount) and is emitted as
`data-skin="iris"` on `<html>`. Resolution is total, like `themeClass`: any
unknown value is graphite. Theme resolution: a recognised `sb-t` cookie, else
`DefaultTheme`, else the skin default (graphite dark, iris **light**). The theme
toggle keeps working in both skins.

**Backward compatibility is byte-for-byte.** A panel that never sets `Skin`
renders identical HTML (layout, login, MFA: goldens in `shell/testdata/`, captured
before the skin existed), identical inline CSS, and requests no fonts. Iris CSS,
tokens and `@font-face` are emitted only for a skinned page, every rule under
`:root[data-skin=iris]` / `:root.dark[data-skin=iris]`. `.btn-soft` is the one
shared addition and lives in `pm7.css` (a static asset), so the HTML stays identical.
A consumer's own `LoginTempl` is its own page and is not skinned.

### Settings area and the theme choice

`Panel.MountSettings(resource.SettingsConfig{...})` mounts a settings *area*, the
GitHub / Linear / Stripe shape: `{base}/settings` redirects (302) to the first page
the operator may see, each page is `{base}/settings/<id>/`, and a left
sub-navigation lists them in two groups (a horizontally scrolling row at ≤767px).
It also points the sidebar's «Настройки» item at the area: `ProfileConfig.SettingsURL`
when the consumer left it empty, and an item of its own in the single-user sidebar.
A panel that never calls `MountSettings` serves no route, renders no link and is
byte-identical to before (goldens in `shell/testdata/`; `TestSettings_NotMountedMeansNoRouteNoLink`).

| Group | Page | Shown when |
|---|---|---|
| «Личные» | `profile`: «Вы вошли как …», name, e-mail, role (read-only), Выйти | a per-operator session (BcryptTOTPAuth) |
| | `security`: change password; two-factor status + Включить / Отключить / Новые коды; «Выйти на всех устройствах, кроме этого» | password needs `Store`; 2FA needs `MountTOTPEnrollment`; sessions needs a store implementing `auth.SessionRevoker` |
| | `appearance`: «Тема» (skin cards with swatches) and «Режим» (Светлая / Тёмная) | always; «Тема» only when more than one skin is allowed |
| «Рабочее пространство» | pages the consumer registers (`SettingsPage`), in registration order | the page's `RequiredRole` |

```go
p.MountSettings(resource.SettingsConfig{
    Store: accountStore, // profile e-mail, password change, sessions (the BcryptTOTPAuth store)
    Pages: []resource.SettingsPage{{
        ID: "collection", Title: "Сбор данных", RequiredRole: auth.RoleOwner,
        BodyFunc: func(r *http.Request) templ.Component { return collectionBody(p.CSRFToken(r)) },
    }},
})
```

A consumer page's POSTs are mounted with `Panel.MountSettingsAction(pageID, ActionSpec{Path: "toggle", ...})`:
the route is `{base}/settings/<pageID>/toggle`, and it **inherits the page's `RequiredRole`** (an
action naming a different role panics), plus the guard and CSRF, so a page's writes are never weaker
than its reads. Role gating happens twice for reads: the route is `guard`ed (403 for a wrong role)
and the nav only lists pages the role may open.

**Primitives** (package `components`, styled in `pm7.css` for both skins, replacing
hand-rolled `set-section` / `set-row` / `sw` (go-nerv's own `nerv.css` defines those names, so go-panel's classes are namespaced `gp-set-*`, `gp-sw*`, `gp-seg-*`; a consumer migrates by swapping its markup for the components, not by overriding CSS)): `SettingsSection{Title, Subtitle, Rows, Body}`
with `SettingsSectionView`, `SettingsRow{Label, Hint, Control}` (a zero row is skipped, a
section with nothing to show renders nothing), `Switch{Action, Token, Fields, On, Label, ...}`
with `SwitchView` (a `role="switch"` button in a CSRF form, optional htmx swap),
`SettingsLayout` / `SettingsLayoutView` (the frame). `shell.ThemeRows(ctx)` returns the two
appearance rows for a consumer that wants them on its own page.

**Password change and sign-out-elsewhere** need a store implementing `auth.EpochStore`
(`PgxAccountStore` does); a store that does not gets neither row (fail closed, no racy fallback).
Both POSTs: CSRF, then a per-account throttle (`SettingsConfig.RateLimiter` / `PasswordRate`,
default in-process 5 per 15 min, fail-closed on a limiter error; with a limiter that implements
`auth.BlockedChecker`, which the default does, only FAILED current-password checks count), then the
current password verified with bcrypt (constant-time; the stored row's ID must equal the session's;
a store outage is an error, not "wrong password"). The password change then applies the minimum
policy (`auth.MinPasswordLength` 10 characters, `auth.MaxPasswordBytes` 72). No password or hash
is logged, echoed or put in an error.

The write is a **conditional, atomic epoch stamp**: `UPDATE … SET password_changed_at = now() [, password_hash = …]
WHERE id = $1 AND (password_changed_at IS NULL OR password_changed_at <= <the calling session's iat>) RETURNING password_changed_at`.
The caller's iat comes from the validated session (`auth.Session.IssuedAtMs`). Zero rows means the
session was revoked after `Require` admitted it, or another change won: nothing is written, nothing
is minted, the operator is sent to login. That closes two races a read-then-write cannot: a stolen
session held in flight cannot restamp the epoch and mint itself a fresh session above the victim's
password change, and two concurrent password changes cannot both succeed.

`password_changed_at` revokes every session issued before it, the caller's included, so the handler
replaces the caller's session with `BcryptTOTPAuth.RefreshSession(w, r, epoch)`: identity from the
request's own signed cookie (no userID parameter), issue time `epoch + 1ms` from the RETURNING value,
never a later re-read. If the replacement cannot be minted the page says «Пароль изменён, войдите
заново» rather than showing success. A per-device session list needs server-side session storage:
phase 2.

**Skins** come from one registry, `shell/skins.go`: name, label per language, default
theme, swatch colours (held to the real tokens by `TestSkinRegistry_SwatchesMatchTheRealTokens`)
and the `<head>` CSS. `resolveSkin`, the cookie whitelist, the options and the layout's CSS
derive from it; a new skin is one entry plus its CSS. `Config.Skins` lists the skins operators
may choose (the default `Config.Skin` is always offered, first; an unknown name panics in `New`).
The choice is cookie **`sb-k`** (not `sb-s`: that is the submenu cookie), one year, written by
`admin.js` on change followed by a reload (a skin's CSS exists only in a page rendered in that
skin). It is read in one place, `Panel.skinFor`, and must EQUAL an offered skin's name; anything
else, including a registered but not offered name, falls back to the panel default. The value
never reaches markup (the rendered `data-skin` comes from the registry). Every page resolves it
through `chromeStateFrom` (`RenderPage`, list, detail, forms, validation re-render, Trash); login
and MFA get it from `withSkin`, which is on for a panel that is skinned **or** offers a choice;
`RenderError` follows it when the caller passes the request's context. The light/dark toggle is
independent (`sb-t`); the Режим radios and the sidebar toggle share `setTheme`, so they stay in step.

**Roadmap (phase 2, not built):** profile editing with confirmation, a device list, notification
channels, interface language, an audit log, and a shared staff / roles / invitations primitive.

### Iris tokens

Light is `:root[data-skin=iris]` (and the base of dark); dark overlays it.

| Token | Light | Dark | Role |
|---|---|---|---|
| `--bg-deep`, `--bg-base` | `#eef0f6` | `#0f1120` | canvas: page and sidebar ground |
| `--bg-raised` (`--bg-surface`) | `#ffffff` | `#171a2c` | tile (radius 20, no border, `--shadow-tile`) |
| `--bg-elevated` | `#f4f5fa` | `#1e2236` | row hover, disabled |
| `--border` / `--border-subtle` | `#e4e7ef` / `#ebedf4` | `#262a40` / `#20243a` | hairlines, ghost button and chip edge |
| `--border-control` | `#7c8299` | `#6b7190` | input edge (3:1) |
| `--track` | `#c5cadb` | `#3a3f5c` | funnel track |
| `--text-primary` | `#141729` | `#eceef6` | ink |
| `--text-secondary` | `#4b516a` | `#b4b9cf` | ink-2 |
| `--text-muted` (`--text-label`) | `#62687f` | `#9aa0b8` | ink-3: captions, column heads, counts |
| `--text-faint` | `#8a90a8` | `#6a708b` | decorative only |
| `--accent` | `#5046e4` | `#8b84ff` | focus ring, active nav icon, bars |
| `--accent-text` | `#3b32c4` | `#b9b4ff` | links, active chip text |
| `--accent-soft` | `#edecfd` | `#24224a` | active chip, `.btn-soft` |
| `--btn-primary-bg` / `-hover` / `--on-btn-primary` | `#141729` / `#2a2f4a` / `#fff` | `#eceef6` / `#fff` / `#0f1120` | primary = ink fill, one per screen |
| `--night`, `--on-night`, `--on-night-2` | `#151932`, `#fff`, `#b8bdda` | `#2a2e63`, `#fff`, `#b8bdda` | focus tile: `.card-focus` |

Status roles (ink text / soft background / signal dot):

| Role | Light | Dark |
|---|---|---|
| positive (ok) | `#17714a` / `#e5f5ec` / `#2e9e6b` | `#5dd3a0` / `#10301f` / `#4cc38a` |
| warning (wait) | `#8f5a07` / `#fcf2df` / `#c98410` | `#f0b54d` / `#33280e` / `#f0b54d` |
| negative (urgent) | `#c22b38` / `#fdecee` / `#e5484d` | `#ff8c94` / `#3a1a22` / `#ff6b73` |
| info (iris) | `#3b32c4` / `#edecfd` / `#5046e4` | `#b9b4ff` / `#24224a` / `#8b84ff` |
| special (orchid; replaces purple/orange/teal) | `#7b3fa0` / `#f5e9fa` / `#a55cc4` | `#e0a8f0` / `#36203f` / `#c77ddb` |
| neutral | `#4b516a` / `#e9ecf4` / `#7d839c` | `#b4b9cf` / `#262a40` / `#8e94ad` |

Type: Manrope Variable (`--font-display`: titles, big numbers, 700/800) and Inter
Variable (`--font-sans`: UI), self-hosted latin + cyrillic woff2 under
`/admin/static/fonts/` (`shell/internal/assets/static/fonts/`, OFL licences beside
them). Radii: control 10 (`--radius`), tile 20 (`--radius-lg`).

### What changes structurally

- Sidebar sits on the canvas; the active item is a white pill with `--shadow-pop`,
  ink text and an iris icon; counts are muted plain numbers; group captions are
  small muted caps.
- Tiles (`.pm7-card`, `.stat-card`, `.kpi-strip`, `.crm-table`, `.occ-board`, ...)
  lose their border and gain `--shadow-tile`; a table nested in a card still draws no second frame.
- Badges are a 7px dot plus a coloured word (`.badge-*` keep working).
- Buttons: ink-fill primary, hairline ghost (`.btn-secondary`), `.btn-soft`
  (accent-soft fill, accent-text) for row actions, danger stays red. Control height 40, 44 on phones.
- Filter chips: white pill; active is accent-soft with iris text.
- `.pm7-card.card-focus` is the dark focus tile (use once per page; `.card-focus-sub`
  for its secondary line). Graphite renders it as a plain card.

### Contrast

`resource/skin_iris_test.go` derives its pairs from the token set (naming
convention, not a list): text tokens on every ground and `--accent-soft`, each
status role on its `-bg` and the grounds, `--on-*` on their surface, button fills
under their label (all >= 4.5), and `--border-control`, `--accent` and every
`--*-signal` on the tile (>= 3). Every iris rule that sets a background and a
colour is checked too. Iris never leaks a graphite colour: every colour token
graphite defines must be defined by the iris block as well (tested).

Deliberate choices: `.btn-secondary` and `.filter-chip` use the hairline
`--border` (text-labelled controls are exempt from WCAG 1.4.11); inputs use
`--border-control`. `--warning-signal` is `#c98410`, darker than the drawn
`#e39b2d` (2.34:1) so the dot clears 3:1.

### Rollout

go-grad switches first (`Skin: shell.SkinIris`); go-nerv, oxpulse-admin and go-job
follow by flipping the same option; making iris the default is a later, separate,
visible change. Try it: `PANEL_SKIN=iris ADMIN_HMAC_KEY=... go run ./example`
(`/admin/today/` is the Today-style page; `PANEL_THEME=dark` overrides the default).

## Migrating from the navy palette (consumers: go-grad, go-job)

No API changed: a zero-value consumer compiles and behaves identically. Only
visuals change when the go-panel version is bumped.

Checked consumers: **go-grad** (v0.23.17) — `var(--text-muted)`, `var(--red)`, `var(--pm7-*)`,
`.badge-*`, `.pm7-card` and `.crm-table` all still resolve; its two
`border-left:3px solid var(--red)` `pm7-card` blocks are go-grad's own and stay
side stripes until go-grad moves them to `components.NoticeView`. **go-job**
(pseudo-version v0.23.13-0.20260928083354-a6ff5313871b) was not verified.
(oxpulse-admin does not import go-panel.)

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

### Occupancy board — `components.OccupancyBoard`

Positions (rows) x months (columns), one bar per `Span`, free gaps as links, a
today line, 6/12/18-month zoom. At ≤767px the board is replaced by a
per-position list (spans and gaps as text). `From` is inclusive, `To` exclusive;
the component never reads the clock (`Today` and `Start` are inputs). Tone is
fill + glyph + visible text (conflict is also hatched); overlapping spans are
stacked in lanes and flagged, never hidden. Uses only existing tokens
(`--positive*`, `--warning*`, `--special*`, `--negative*`, `--neutral*`).

### Unit strip — `components.UnitStrip` / `resource.UnitStripCell`

A wrapping row of small numbered tiles (30px, 4px gap) for ONE row of a list:
the state of places 1..N today. `resource.UnitStripCell(components.UnitRow{...})`
returns the markup for `Cell{HTML: true}`; `components.UnitLegend` lists the
tones once (this repo has no slot for text above a list, so a consumer puts it
in a `MountPage` or a detail section). Use it when many rows each need a
glance; use `OccupancyBoard` when ONE row needs its time axis. Tones are the
closed `UnitTone` set: free (outlined `○`), hold (dashed `◌`), sale `●`,
renewal due `↻`, unverified `?`, conflict (hatched `!`). Hold, sale, renewal
and conflict reuse the `OccupancyBoard` glyphs and the status roles
(`--warning*`, `--positive*`, `--special*`, `--negative*`); unverified is
`--info*`; free is `--bg-raised` + `--border-control`. Tone is fill, glyph and
text (screen-reader label on the tile, visible in the legend); `Note` is short
visible text (truncated with an ellipsis past 10ch), `Title` the tooltip and
screen-reader name: it replaces the default "Place N · tone · note", so a custom
Title must include the state. A value outside the set renders as
unverified, never as free. `Href` is followed only when it is a same-origin
path (`/x`, not `//`, `/\`, a scheme or a control character); otherwise the
tile has no link. Linked tiles take the one focus ring. Inside the stacked-card
phone layout the strip simply wraps within the card.
