# go-panel

Composable htmx admin-UI kit for Go. Sits on go-kit. GOTTH stack (Go + templ + htmx + pm7).

Pick what you need — each package is independently importable with no required entry point.
Mirrors [@krolik/landing-kit](https://github.com/anatolykoptev/landing-kit) philosophy.

## Why this exists — read before hand-rolling an admin

**This is the fleet's ONE admin framework. Do not build a new admin "на коленке".**

Every service that grew its own hand-rolled admin (go-nerv, oxpulse-admin, go-grad all did)
re-invented the same sidebar + table + auth — and each re-introduced the same defects we
have already fixed here, once, with review:

- **`nav-hide ≠ access control`.** A hand-rolled sidebar hides a link by role and *thinks*
  it gated the page — the route stays reachable by direct URL. go-panel derives nav-hide
  from `RequiredRole` **and** enforces it at every route mount (`RoleAuthenticator`), so the
  two can't drift. (This exact hole was found and closed in the role-gating phase.)
- **FOUC on collapse.** localStorage-driven collapse renders expanded, then JS collapses
  post-load — a visible flash. go-panel persists collapse in a **server-readable cookie**
  and renders the collapsed state SSR. No flash.
- **A11y / keyboard / aria-current** — done once here, skipped in every hand-roll.
- **SQL-injection-safe filtering/sorting** — only author-declared `SQLExpr` constants reach
  SQL; URL bytes are bind args only. A hand-rolled table tends to string-concat.
- **ТСПУ-safe assets** — htmx/pm7 served from embedded FS, never a CDN. A hand-roll that
  pulls a CDN script breaks behind Russian DPI.
- **Per-render-N-queries badges** — live nav counts go through `CachedBadge(ttl, fn)`, not a
  COUNT(\*) on every page render.

Hand-rolling means re-deriving all of the above and re-shipping the bugs. **Register a
`Resource` instead** — you get the sidebar, the table (search/filter/sort/pagination/htmx),
CRUD, and auth for free, all review-gated. If the kit is missing something, **extend the kit**
(additive `NavItem`/`Resource` fields are backward-compatible) so every consumer benefits —
that is how the sidebar grew collapse → groups → nesting → drawer → badges → role-gating →
profile, each as one reusable phase rather than five private copies.

**Consumers:** go-job (live). **Migrating off their hand-rolled admins:** go-nerv,
oxpulse-admin, go-grad.

## Packages

| Package | Responsibility | Status |
|---|---|---|
| [`admintable`](./admintable/) | SQL-injection-safe `Spec` (sort) + `FilterSpec` (filter) + pagination | stable |
| [`auth`](./auth/) | Pluggable session: `HMACAuth` (single-user, per-login nonce) + `BcryptTOTPAuth` (multi-user, bcrypt + TOTP 2FA + roles) | stable |
| [`csrf`](./csrf/) | Double-submit CSRF tokens bound to session cookie (32-byte key floor) | stable |
| [`render`](./render/) | htmx fragment vs full-page, goldmark markdown | stable |
| [`resource`](./resource/) | **Core**: `Resource` declaration → list + CRUD form handlers (`Writer`, `OptionsFunc`) + nav entry + role-gating (`RequiredRole`) | stable |
| [`shell`](./shell/) | Layout + full sidebar + static assets (htmx, pm7 CSS/JS). See Sidebar below. | stable |
| [`tenant`](./tenant/) | city_slug scope seam: `Resolver`, `ScopeClause`, `Middleware` | stable |
| `mcp` | Auto-expose a Resource as MCP read/list tools | planned |
| `media` | Upload + crop + imgproxy URL | planned |
| `components` | pm7 widgets: sparkline, badge, pagination | grows with need |

## Sidebar (shell)

The left-nav is **data-driven** — it renders from the `[]NavItem` that `Register` builds out of
your `Resource` declarations (Name/Title/Icon/Group/Badge/Visible/RequiredRole). You declare
data, not markup. Capabilities (all opt-in, zero-value = off, backward-compatible):

- **Icon-rail collapse** — 220↔56px, cookie-persisted, **SSR (no FOUC)**, `aria-current`.
- **Collapsible groups** — group headers click-to-collapse, per-group cookie state, SSR.
- **Nested submenus** — `NavItem.Children` (one level), active-parent auto-expand.
- **Mobile off-canvas drawer** — hamburger + backdrop + Esc below 768px; desktop unchanged.
- **Tooltips** — icon-rail wayfinding in collapsed mode (pure CSS, CSP-clean).
- **Live badges** — `Badge func(ctx) string` rendered as a pill; wrap in `shell.CachedBadge(ttl, fn)`
  so a DB count isn't run on every render.
- **Role-gating** — `Resource.RequiredRole` enforces at the route (403 on direct URL via the
  `RoleAuthenticator` capability) **and** auto-derives nav-hide. `Visible func(ctx) bool` is an
  orthogonal cosmetic filter (feature flags / tenant tier) — never a security authority.
- **Profile block** — sticky-bottom name/role + logout; degrades to logout-only when the auth
  backend has no session role (e.g. `HMACAuth`).

`NavItem`/`Resource` carry function/slice fields → **not comparable** (never use as a map key
or with `==`).

## Quickstart

```go
import (
    "github.com/anatolykoptev/go-panel/admintable"
    "github.com/anatolykoptev/go-panel/auth"
    "github.com/anatolykoptev/go-panel/resource"
    "github.com/anatolykoptev/go-panel/tenant"
)

a := auth.NewHMACAuth(auth.HMACConfig{
    Username: os.Getenv("ADMIN_USER"),
    Password: os.Getenv("ADMIN_PASSWORD"),
    HMACKey:  []byte(os.Getenv("ADMIN_HMAC_KEY")),
    BasePath: "/admin",
    Secure:   true,
})

p := resource.New(resource.Config{
    Title:    "My Admin",
    BasePath: "/admin",
    Auth:     a,
})

resource.Register(p, resource.Resource{
    Name:  "places",
    Title: "Places",
    Icon:  "📍",
    Group: "Content",
    Sort: admintable.Spec{
        Columns: []admintable.Column{
            {Key: "name",    Label: "Name",    Sortable: true, SQLExpr: "p.name"},
            {Key: "updated", Label: "Updated", Sortable: true, SQLExpr: "p.updated_at", NullsLast: true},
        },
        DefaultKey: "updated",
        DefaultDir: admintable.Desc,
    },
    Filter: admintable.FilterSpec{Filters: []admintable.Filter{
        {Key: "status", SQLExpr: "p.status", Match: admintable.Eq, Allowed: []string{"published","draft"}},
        {Key: "q",      SQLExpr: "p.name",   Match: admintable.ILike},
    }},
    Scope: tenant.Scope{Column: "p.city_slug"},
    Perms: resource.ReadAny,
    // Optional: live count pill on the nav item, TTL-bounded.
    Badge: shell.CachedBadge(30*time.Second, func(ctx context.Context) string {
        return strconv.Itoa(store.CountPlaces(ctx))
    }),
    // Optional (BcryptTOTPAuth consumers): gate route + nav by role.
    // RequiredRole: "editor",
    Lister: func(ctx context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
        return store.ListPlaces(ctx, q) // your pgxpool query
    },
})

mux.Handle("/admin/", p.Handler())
```

## Runnable example

```bash
go run ./example
# open http://localhost:8080/admin/  username: admin  password: demo
```

## Layering

```
go-kit (data primitives: admintable Spec/FilterSpec, embed, fileopt)
  ↑
go-panel (admin UI kit: admintable/auth/csrf/render/shell/tenant/resource/writer)
  ↑
apps — go-job (composes the kit). go-nerv / oxpulse-admin / go-grad migrating onto it.
```

## Architecture constraints

- **No framework entry point** — import only what you need.
- **No schema ownership** — `Lister` and `Writer` (CRUD) are app-supplied closures. The kit never assumes a schema.
- **Data-driven nav, not compositional** — you register `Resource`s; the kit builds the sidebar. You do not hand-write nav markup per page. (This is why a wholesale Tailwind/compositional sidebar lib is NOT vendored — it would force every consumer to drop declarative registration.)
- **`shell` is a pure presentation sink** — it imports neither `net/http` nor `auth`. The `resource` layer parses the request (cookies, session) and threads display state via one `ChromeState` context value. Role/auth logic never runs inside the template.
- **No CDN** — all static assets (htmx.min.js, pm7 CSS/JS) served from embedded FS. Safe behind ТСПУ.
- **Tenant scope is unconditional** — a Resource with `Scope.Column != ""` always gets the WHERE injected. Cannot be bypassed per-call.
- **SQL safety via admintable** — only author-declared `SQLExpr` compile-time constants + literal operators reach SQL. URL bytes are bind args only.
- **Role-gating is fail-closed** — set `RequiredRole` against an auth that can't enforce roles and `Register` panics; a hidden nav link is always backed by a real route gate.

## Development

```bash
make generate  # templ generate (edit *.templ → regen *_templ.go → commit BOTH)
make build     # go build ./...
make test      # go test -race ./...
make lint      # golangci-lint run ./...
make check     # lint + test
```

Releases run via the fleet's local release-please driver (GitHub Actions runner minutes are
billing-gated), not GHA. Extend the kit with **additive** `NavItem`/`Resource` fields to keep
every consumer backward-compatible.

## License

MIT
