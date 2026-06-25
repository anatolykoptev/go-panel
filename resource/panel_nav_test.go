package resource_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
	"github.com/anatolykoptev/go-panel/tenant"
)

// minResource returns a minimal valid Resource suitable for Register calls in
// nav-focused tests.
func minResource(name string) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: name,
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "t.id"}},
			DefaultKey: "id",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{},
		Scope:  tenant.Scope{},
		Perms:  resource.ReadAny,
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		},
	}
}

// TestAddNav_AppendsAfterResource verifies that AddNav appends a bare item to
// p.nav and that it appears after the resource's own nav entry.
func TestAddNav_AppendsAfterResource(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, minResource("widgets"))

	custom := shell.NavItem{
		ID:    "dashboard",
		Label: "Dashboard",
		URL:   "/admin/dashboard",
	}
	p.AddNav(custom)

	nav := p.NavItems()

	// Find indices.
	widgetIdx := -1
	dashIdx := -1
	for i, n := range nav {
		switch n.ID {
		case "widgets":
			widgetIdx = i
		case "dashboard":
			dashIdx = i
		}
	}

	if widgetIdx < 0 {
		t.Fatal("widgets nav item not found")
	}
	if dashIdx < 0 {
		t.Fatal("dashboard nav item not found after AddNav")
	}
	if dashIdx <= widgetIdx {
		t.Errorf("expected dashboard (idx %d) to appear after widgets (idx %d)", dashIdx, widgetIdx)
	}

	// Verify the item content is preserved.
	got := nav[dashIdx]
	if got.Label != "Dashboard" {
		t.Errorf("expected Label=Dashboard, got %q", got.Label)
	}
	if got.URL != "/admin/dashboard" {
		t.Errorf("expected URL=/admin/dashboard, got %q", got.URL)
	}
}

// TestAddNav_GroupHeaderAndLink verifies that AddNav correctly inserts a
// group-header NavItem followed by a link item, and that the group header has a
// non-empty Group field.
func TestAddNav_GroupHeaderAndLink(t *testing.T) {
	p := newTestPanel()

	header := shell.NavItem{
		ID:    "group:Tools",
		Group: "Tools",
	}
	link := shell.NavItem{
		ID:    "my-tool",
		Label: "My Tool",
		URL:   "/admin/my-tool",
	}

	p.AddNav(header)
	p.AddNav(link)

	nav := p.NavItems()

	headerIdx := -1
	linkIdx := -1
	for i, n := range nav {
		switch n.ID {
		case "group:Tools":
			headerIdx = i
		case "my-tool":
			linkIdx = i
		}
	}

	if headerIdx < 0 {
		t.Fatal("group header not found in nav")
	}
	if linkIdx < 0 {
		t.Fatal("link item not found in nav")
	}
	if linkIdx <= headerIdx {
		t.Errorf("expected link (idx %d) to appear after group header (idx %d)", linkIdx, headerIdx)
	}

	// Group header must have a non-empty Group field.
	groupHeader := nav[headerIdx]
	if groupHeader.Group == "" {
		t.Errorf("group header item has empty Group field")
	}
}

// TestNavItemsActive_MarksCorrectItem verifies that calling NavItemsActive
// returns a copy where exactly the matching item is Active and all others are
// not.
func TestNavItemsActive_MarksCorrectItem(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, minResource("alpha"))
	resource.Register(p, minResource("beta"))

	active := p.NavItemsActive("alpha")

	for _, n := range active {
		if n.ID == "alpha" {
			if !n.Active {
				t.Errorf("expected item %q to be Active", n.ID)
			}
		} else {
			if n.Active {
				t.Errorf("expected item %q to be inactive, but Active=true", n.ID)
			}
		}
	}
}

// TestNavItemsActive_UnknownID verifies that passing a non-existent ID results
// in all items having Active=false.
func TestNavItemsActive_UnknownID(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, minResource("alpha"))
	resource.Register(p, minResource("beta"))

	active := p.NavItemsActive("does-not-exist")

	for _, n := range active {
		if n.Active {
			t.Errorf("expected all items inactive for unknown ID, but item %q has Active=true", n.ID)
		}
	}
}

// TestNavItemsActive_ReturnsCopy verifies that the slice returned by
// NavItemsActive is a copy — mutating it does not affect the panel's internal
// state.
func TestNavItemsActive_ReturnsCopy(t *testing.T) {
	p := newTestPanel()
	resource.Register(p, minResource("alpha"))

	snap1 := p.NavItemsActive("alpha")
	// Mutate the returned copy.
	for i := range snap1 {
		snap1[i].Active = false
		snap1[i].Label = "mutated"
	}

	// A fresh call must still return the correct state, unaffected by the mutation.
	snap2 := p.NavItemsActive("alpha")
	found := false
	for _, n := range snap2 {
		if n.ID == "alpha" {
			found = true
			if !n.Active {
				t.Errorf("NavItemsActive should return a copy; mutation of previous result affected state")
			}
			if n.Label == "mutated" {
				t.Errorf("NavItemsActive returned a reference, not a copy: label was mutated")
			}
		}
	}
	if !found {
		t.Fatal("alpha item not found in second NavItemsActive call")
	}
}
