# go-panel

Composable htmx admin-UI kit for Go. Sits on go-kit. GOTTH stack (Go + templ + htmx).

Pick what you need — each package is independently importable with no required entry point.
Mirrors [@krolik/landing-kit](https://github.com/anatolykoptev/landing-kit) philosophy.

## Packages

| Package | Responsibility | Status |
|---|---|---|
| [`admintable`](./admintable/) | SQL-injection-safe `Spec` (sort) + `FilterSpec` (filter) | foundations |
| [`auth`](./auth/) | Pluggable session: `HMACAuth` (single-user) + `BcryptTOTPAuth` stub | foundations |
| [`render`](./render/) | htmx fragment vs full-page, goldmark markdown | foundations |
| [`shell`](./shell/) | Layout + sidebar nav + static assets (htmx, pm7 CSS/JS) | foundations |
| [`tenant`](./tenant/) | city_slug scope seam: `Resolver`, `ScopeClause`, `Middleware` | foundations |
| [`resource`](./resource/) | **Core**: `Resource` declaration → list handler + nav entry | foundations (list only) |
| `form` | CRUD form rendering + validation | Phase 2 |
| `mcp` | Auto-expose a Resource as MCP read/list tools | Phase 3 |
| `media` | Upload + crop + imgproxy URL | Phase 4 |
| `components` | pm7 widgets: sparkline, badge, pagination | grows with need |

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
go-kit (data primitives: admintable Spec/FilterSpec)
  ↑
go-panel (admin UI kit: shell/auth/render/tenant/resource)
  ↑
apps (go-piter, oxpulse-admin, go-nerv — compose the kit)
```

## Architecture constraints

- **No framework entry point** — import only what you need.
- **No schema ownership** — `Lister` and (future) `Writer` are app-supplied closures. The kit never assumes a schema.
- **No CDN** — all static assets (htmx.min.js, pm7 CSS/JS) served from embedded FS. Safe behind ТСПУ.
- **Tenant scope is unconditional** — a Resource with `Scope.Column != ""` always gets the WHERE injected. Cannot be bypassed per-call.
- **SQL safety via admintable** — only author-declared `SQLExpr` compile-time constants + literal operators reach SQL. URL bytes are bind args only.

## Development

```bash
make generate  # templ generate
make build     # go build ./...
make test      # go test -race ./...
make lint      # golangci-lint run ./...
make check     # lint + test
```

## License

MIT
