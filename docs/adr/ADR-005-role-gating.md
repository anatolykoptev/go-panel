# ADR-005 — Role-gated resources via the RoleAuthenticator capability

- Status: **Accepted** (2026-06-27)
- Deciders: operator
- Relates: ADR-003 (go-panel as the durable framework core); ADR-004 (reusable Detailer Show-view)

## Numbering note

This ADR is numbered **005**. The role-gating plan originally reserved ADR-004,
but ADR-004 was taken by the Detailer Show-view ADR (merged in #27) before this
work landed. ADR numbers are immutable and unique, so role-gating takes the next
free number rather than colliding with an existing 004.

## Context

go-panel's `resource.Resource` mounts list, detail, create, and edit routes behind
a single `Authenticator.Require` gate: any authenticated operator may reach every
resource. As admin surfaces grow (go-job, go-nerv, go-startup), some resources hold
data or actions that only a subset of operators should reach — an `admin`-only
settings table, an `owner`-only billing view.

The `BcryptTOTPAuth` authenticator already carries per-session roles and a
`RequireRole(role, next)` route wrapper (with an `owner` super-role that is always
permitted). What was missing is a **framework seam** that lets a `Resource` declare
the role it needs and have go-panel enforce it on every route — and a parallel
read-only derivation the sidebar can use to hide a link the operator cannot open.

Two failure modes had to be designed out:

1. **Fail-open gating** — a resource declaring a required role mounted against an
   authenticator that cannot enforce roles would silently serve ungated. This is the
   classic "validated-but-inert security field" smell.
2. **Authority confusion** — using a nav-hide check (`HasRole`) as the route gate.
   Hiding a link is UX, not security; the route itself must be independently gated.

## Decision

Add an **optional capability interface** in the `resource` package and a
**nil/empty-default** field on `Resource`:

```go
type RoleAuthenticator interface {
    RequireRole(role string, next http.HandlerFunc) http.HandlerFunc // route AUTHORITY
    HasRole(ctx context.Context, role string) bool                   // nav-hide derivation
}
```

- `Resource.RequiredRole string` (empty = no gate, the foundational behaviour).
- `Panel.guard(requiredRole, h)` is the single wrapper every resource route is mounted
  through. For an empty role it is exactly `p.auth.Require(h)` — zero behaviour change
  for the existing fleet. For a non-empty role it asserts the `RoleAuthenticator`
  capability and returns `ra.RequireRole(role, h)`.
- `validateRoleConfig(p, r)` runs at `Register` time, next to `validateWriterConfig`,
  and **panics (fail-closed)** if `RequiredRole` is non-empty but the authenticator does
  not implement `RoleAuthenticator`. This closes failure mode 1: a role-gated resource
  can never mount ungated. It mirrors the existing fail-closed Writer/CSRF validation
  idiom (`SEC-CR-001`).
- `BcryptTOTPAuth` gains `HasRole(ctx, role)` (three lines): `SessionFrom(ctx)` then
  `role == s.Role || s.Role == RoleOwner`; returns `false` (never panics) with no session.
  With its pre-existing `RequireRole`, `BcryptTOTPAuth` now structurally satisfies
  `RoleAuthenticator`.

### Authority boundary (failure mode 2)

`RequireRole` is the **only** enforcement authority. `HasRole` is documented and intended
solely for nav-hiding — never as the check standing between a request and a protected
route. A future sidebar nav-hide consumer uses `HasRole`; the route stays gated by
`RequireRole` regardless of whether the link is rendered.

### Capability, not interface widening

`RoleAuthenticator` is a separate optional interface, **not** a new method on the core
`Authenticator`. The `HMACAuth` single-user authenticator (used by go-job) has no concept
of roles, does not implement the capability, and sets no `RequiredRole` — it is wholly
unaffected (a no-op). Only authenticators that opt in by implementing both methods can
back a role-gated resource. This mirrors the existing `sessionCookier` optional-capability
pattern already used for CSRF session binding.

## Consequences

**Forward-only API surface**: `RoleAuthenticator` (public interface) and
`Resource.RequiredRole` (public field) are new public surface. Removing them after
adoption is a breaking library change + release — hence the ADR ceremony. The interface
shape is the load-bearing, one-way decision: two methods, `RequireRole` as authority and
`HasRole` as derivation.

**Positive**: every go-panel-backed admin can role-gate a resource with one field, fully
enforced on every route, fail-closed by construction, with no per-admin middleware. The
same field powers nav-hiding without a second source of truth.

**Trade-offs**: +1 public interface, +1 public field, +1 startup validation, +1 method on
`BcryptTOTPAuth`. Zero cost and zero behaviour change for resources that declare no
`RequiredRole` (the entire current fleet, including go-job/HMACAuth).

## Alternatives considered

1. **Add `RequireRole`/`HasRole` to the core `Authenticator` interface.** Rejected: it
   would force every authenticator (including the role-less `HMACAuth`) to implement role
   methods, and a stub implementation would be a fail-open trap. The optional-capability
   pattern keeps role-less authenticators honest and unchanged.
2. **Per-admin role middleware mounted around the panel.** Rejected — bypasses framework
   nav/layout/validation, does not generalize, and re-introduces the duplication ADR-003
   exists to prevent.
3. **Enforce gating via `HasRole` inside each handler.** Rejected — conflates nav-hide
   derivation with the enforcement authority (failure mode 2) and scatters the check across
   handlers instead of one `guard` seam.
