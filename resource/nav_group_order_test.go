package resource_test

import (
	"reflect"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

// probeRes returns a minimal valid Resource with the given name and sidebar
// group, suitable for Register calls in nav-grouping tests. It reuses
// minResource (from panel_nav_test.go) and sets Group.
func probeRes(name, group string) resource.Resource {
	r := minResource(name)
	r.Group = group
	return r
}

// groupMembership builds a map of group-label → set-of-item-IDs from the
// panel's nav, using shell.NavGroups — the REAL toNavGroups logic, not a
// reimplementation. The anonymous pre-group bucket is keyed "".
func groupMembership(t *testing.T, p *resource.Panel) map[string]map[string]bool {
	t.Helper()
	groups := shell.NavGroups(p.NavItems(), nil)
	m := make(map[string]map[string]bool, len(groups))
	for _, g := range groups {
		ids := make(map[string]bool, len(g.Items))
		for _, item := range g.Items {
			ids[item.ID] = true
		}
		m[g.Label] = ids
	}
	return m
}

// expectedMembership is the canonical grouping for the test resource set,
// independent of registration order. Every order must produce this.
var expectedMembership = map[string]map[string]bool{
	"Inventory": {"zones": true, "expiring_placements": true, "active_ads": true},
	"Money":     {"clients": true, "leads": true, "billable_leads": true},
	"CRM":       {"contacts": true},
}

// allResources returns the full test resource set (3 groups, 7 resources).
// Callers register them in various orders.
func allResources() []resource.Resource {
	return []resource.Resource{
		probeRes("zones", "Inventory"),
		probeRes("expiring_placements", "Inventory"),
		probeRes("active_ads", "Inventory"),
		probeRes("clients", "Money"),
		probeRes("leads", "Money"),
		probeRes("billable_leads", "Money"),
		probeRes("contacts", "CRM"),
	}
}

// registerAll calls Register for each resource in the given order.
func registerAll(p *resource.Panel, ress ...resource.Resource) {
	for _, r := range ress {
		resource.Register(p, r)
	}
}

// assertMembershipEquals checks that the panel's group membership matches
// expected exactly (same groups, same member sets).
func assertMembershipEquals(t *testing.T, p *resource.Panel, expected map[string]map[string]bool) {
	t.Helper()
	got := groupMembership(t, p)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("group membership mismatch:\n  got=%v\n  want=%v", got, expected)
	}
}

// ── F1: grouping is order-independent ─────────────────────────────────────

// TestNavGrouping_OrderIndependent registers the same resource set in four
// different orders (contiguous, interleaved, reverse, manual-header-first)
// and asserts the resulting group membership is identical every time —
// compared as sets, so within-group ordering does not make the test brittle.
//
// Falsification (F1): in resource/resource.go addNavEntry, restore the
// append-at-end behaviour for the case where the group header already exists
// (replace the slices.Insert branch with p.nav = append(p.nav, link)).
// The interleaved and reverse orders will produce different groupings than
// the contiguous order → RED.
func TestNavGrouping_OrderIndependent(t *testing.T) {
	orders := []struct {
		name string
		reg  func(p *resource.Panel)
	}{
		{
			name: "contiguous",
			reg: func(p *resource.Panel) {
				registerAll(p,
					probeRes("zones", "Inventory"),
					probeRes("expiring_placements", "Inventory"),
					probeRes("active_ads", "Inventory"),
					probeRes("clients", "Money"),
					probeRes("leads", "Money"),
					probeRes("billable_leads", "Money"),
					probeRes("contacts", "CRM"),
				)
			},
		},
		{
			name: "interleaved",
			reg: func(p *resource.Panel) {
				registerAll(p,
					probeRes("clients", "Money"),
					probeRes("contacts", "CRM"),
					probeRes("zones", "Inventory"),
					probeRes("leads", "Money"),
					probeRes("expiring_placements", "Inventory"),
					probeRes("active_ads", "Inventory"),
					probeRes("billable_leads", "Money"),
				)
			},
		},
		{
			name: "reverse",
			reg: func(p *resource.Panel) {
				registerAll(p,
					probeRes("contacts", "CRM"),
					probeRes("billable_leads", "Money"),
					probeRes("leads", "Money"),
					probeRes("clients", "Money"),
					probeRes("active_ads", "Inventory"),
					probeRes("expiring_placements", "Inventory"),
					probeRes("zones", "Inventory"),
				)
			},
		},
		{
			name: "manual_header_first",
			reg: func(p *resource.Panel) {
				// Manual AddNav group header for Inventory placed first.
				p.AddNav(shell.NavItem{ID: "group:Inventory", Group: "Inventory"})
				// Then all resources in interleaved order — Inventory's
				// resources are registered LAST, not right after their header.
				registerAll(p,
					probeRes("clients", "Money"),
					probeRes("leads", "Money"),
					probeRes("contacts", "CRM"),
					probeRes("billable_leads", "Money"),
					probeRes("zones", "Inventory"),
					probeRes("expiring_placements", "Inventory"),
					probeRes("active_ads", "Inventory"),
				)
			},
		},
	}

	var baseline map[string]map[string]bool
	for _, tc := range orders {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPanel()
			tc.reg(p)
			got := groupMembership(t, p)

			if baseline == nil {
				baseline = got
			} else if !reflect.DeepEqual(got, baseline) {
				t.Fatalf("order %q produced different grouping than the first order:\n  got=%v\n  first=%v", tc.name, got, baseline)
			}

			// Also assert against the canonical expected membership.
			assertMembershipEquals(t, p, expectedMembership)
		})
	}
}

// ── F2: a manual AddNav group header is honoured ──────────────────────────

// TestNavGrouping_ManualHeaderHonoured registers a manual AddNav group
// header for Inventory FIRST, then registers other groups' resources, and
// only THEN registers Inventory's resources last. Asserts Inventory's
// resources land under the manual header — not stranded under whichever
// group was open at the end.
//
// Falsification (F2): same mutation as F1 (restore append-at-end when the
// header already exists). Inventory's resources will be appended at the
// end of p.nav, landing under the CRM group (the last opened) → RED.
func TestNavGrouping_ManualHeaderHonoured(t *testing.T) {
	p := newTestPanel()
	p.AddNav(shell.NavItem{ID: "group:Inventory", Group: "Inventory"})
	// Other groups first.
	registerAll(p,
		probeRes("clients", "Money"),
		probeRes("leads", "Money"),
		probeRes("billable_leads", "Money"),
		probeRes("contacts", "CRM"),
	)
	// Inventory's resources LAST — the edge case that traps today.
	registerAll(p,
		probeRes("zones", "Inventory"),
		probeRes("expiring_placements", "Inventory"),
		probeRes("active_ads", "Inventory"),
	)

	got := groupMembership(t, p)
	// Inventory must contain exactly its three resources.
	inv, ok := got["Inventory"]
	if !ok {
		t.Fatalf("Inventory group not found; got groups=%v", got)
	}
	expectedInv := map[string]bool{"zones": true, "expiring_placements": true, "active_ads": true}
	if !reflect.DeepEqual(inv, expectedInv) {
		t.Fatalf("Inventory membership wrong (manual header not honoured):\n  got=%v\n  want=%v", inv, expectedInv)
	}
	// And the other groups must be correct too.
	assertMembershipEquals(t, p, expectedMembership)
}

// ── F3: a contiguous consumer is unaffected ───────────────────────────────

// TestNavGrouping_ContiguousUnchanged registers all resources contiguously
// (each group's members together, in group order) and asserts the resulting
// flat NavItems slice matches what origin/main produces for the same input,
// element for element. This is the compatibility guard — three consumers
// register contiguously and none asked for a reshuffle.
//
// Falsification (F3): in addNavEntry, insert the link at headerIdx+1
// (beginning of the group's run) instead of after existing members. This
// reverses within-group registration order for contiguous consumers, so the
// slice no longer matches the expected origin/main output → RED.
func TestNavGrouping_ContiguousUnchanged(t *testing.T) {
	p := newTestPanel()
	registerAll(p, allResources()...)

	// This is exactly what origin/main's addNavEntry produces for the same
	// contiguous registration: each group header immediately followed by
	// its members in registration order.
	expected := []shell.NavItem{
		{ID: "group:Inventory", Group: "Inventory"},
		{ID: "zones", Label: "zones", URL: "/admin/zones"},
		{ID: "expiring_placements", Label: "expiring_placements", URL: "/admin/expiring_placements"},
		{ID: "active_ads", Label: "active_ads", URL: "/admin/active_ads"},
		{ID: "group:Money", Group: "Money"},
		{ID: "clients", Label: "clients", URL: "/admin/clients"},
		{ID: "leads", Label: "leads", URL: "/admin/leads"},
		{ID: "billable_leads", Label: "billable_leads", URL: "/admin/billable_leads"},
		{ID: "group:CRM", Group: "CRM"},
		{ID: "contacts", Label: "contacts", URL: "/admin/contacts"},
	}

	got := p.NavItems()
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("contiguous nav slice changed (compat broken):\n  got=%v\n  want=%v", got, expected)
	}
}
