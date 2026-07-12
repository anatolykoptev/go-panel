# Contributing

Internal/proprietary repo. House rules:

## Quality gate (required before every PR)

```
make check        # build + vet + test -race + golangci-lint + govulncheck
```

CI (`.github/workflows/ci.yml`) runs the same gate. A PR is not mergeable until it
is green.

## Commits & releases

- **Conventional Commits** (`feat:`, `fix:`, `chore:`, `docs:`, `test:`, …).
  release-please derives the next version + CHANGELOG from these, so the prefix is
  load-bearing: `feat` → minor, `fix` → patch.
- Releases are cut by merging the release-please PR (it tags `vX.Y.Z`). Consumers
  pin the **tag**, never a SHA pseudo-version.

## Layering rules (do not break)

- The core `identity` package stays **dependency-light**: interfaces only for
  `SessionStore`, `RateLimiter`, `Observer`, `EmailSender`, `UserStore`.
- Concrete impls that pull a heavy dependency live in their **own subpackage** so a
  host that does not use them never compiles the dependency:
  - `identity/session` (Redis sessions), `identity/ratelimit` (Redis limiter) —
    redis.
  - `identity/promobs` (Prometheus observer) — client_golang.
- A host implements exactly one seam itself: `identity.UserStore` (over its DB).

## Panics vs errors

- **Setup-time constructors panic on programmer/config error.** `resource.New`,
  `resource.Register`, `Panel.MountPage`, `resource.MountTOTPEnrollment`,
  `auth.NewHMACAuth`, `auth.NewBcryptTOTPAuth`, and `semantic.New` all panic on
  a nil dependency, short key, or invalid spec — deliberate fail-closed-at-setup
  behavior, the same convention `net/http.ServeMux.Handle` uses for a duplicate
  route registration. A misconfiguration is a bug to fix in code, not a runtime
  condition callers branch on, and it only fires during setup, never per-request.
- **`identity.New` is the one deliberate exception** — it returns `error`
  instead, because go-grad already handles that error on its money-path. Do not
  retrofit it to panic, and do not add a `Build() (Panel, error)` alternative to
  the panic-based constructors above; both were evaluated and rejected as churn
  without a safety benefit (repo-review-council audit, 2026-07-10).

## Worktrees

Do feature work in a `git worktree`, never on the main checkout. One PR per change.
