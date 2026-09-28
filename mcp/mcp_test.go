package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubResource builds a minimal Resource with a Lister and Detailer for testing.
func stubResource(name string) resource.Resource {
	return resource.Resource{
		Name:  name,
		Title: "Test " + name,
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", SQLExpr: "id", Sortable: true},
			},
			DefaultKey: "id",
			DefaultDir: admintable.Desc,
		},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{
				{ID: "1", Cells: []resource.Cell{{Value: "row 1"}}},
				{ID: "2", Cells: []resource.Cell{{Value: "row 2"}}},
			}, 2, nil
		},
		Detailer: func(_ context.Context, _ *http.Request, id string) ([]resource.DetailSection, error) {
			return []resource.DetailSection{
				{Title: "Info", Items: []resource.DetailItem{{Label: "ID", Value: id}}},
			}, nil
		},
	}
}

func TestRowsToJSON(t *testing.T) {
	rows := []resource.Row{
		{ID: "1", Cells: []resource.Cell{{Value: "alpha", HTML: false}}, Href: "/admin/test/1"},
		{ID: "2", Cells: []resource.Cell{{Value: "<b>beta</b>", HTML: true}}},
	}
	out := rowsToJSON(rows)
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}
	if out[0].ID != "1" || out[0].Href != "/admin/test/1" {
		t.Errorf("row 0: unexpected ID=%q Href=%q", out[0].ID, out[0].Href)
	}
	if out[1].Cells[0].HTML != true {
		t.Errorf("row 1 cell 0: expected HTML=true, got false")
	}
}

func TestSectionsToJSON(t *testing.T) {
	sections := []resource.DetailSection{
		{Title: "Card", Items: []resource.DetailItem{{Label: "Name", Value: "Test", HTML: false}}},
		{RawHTML: "<div>custom</div>"},
	}
	out := sectionsToJSON(sections)
	if len(out) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(out))
	}
	if out[0].Title != "Card" || len(out[0].Items) != 1 {
		t.Errorf("section 0: unexpected Title=%q items=%d", out[0].Title, len(out[0].Items))
	}
	if out[1].RawHTML != "<div>custom</div>" {
		t.Errorf("section 1: unexpected RawHTML=%q", out[1].RawHTML)
	}
}

func TestRegisterResourceTools(t *testing.T) {
	// Verify that registerResourceTools does not panic and handles
	// resources with and without Detailer, with or without a TenantResolver.
	resources := []resource.Resource{
		stubResource("with_detail"),
		{Name: "no_detail", Title: "No Detail", Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return nil, 0, nil
		}},
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerResourceTools panicked: %v", r)
		}
	}()
	registerResourceTools(server, resources, slog.Default(), nil)
}

// connectTools registers resources (with the given TenantResolver, possibly
// nil) on an in-process MCP server and returns a connected client session.
func connectTools(t *testing.T, resources []resource.Resource, tr func(context.Context) (tenant.Tenant, bool)) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	registerResourceTools(server, resources, slog.New(slog.DiscardHandler), tr)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestCallTenant_LegacyNilResolver pins the pre-resolver behavior: tenant.From
// (fail-open global) and an UNSTAMPED ctx — byte-for-byte compatibility for
// single-tenant deployments that never configure Config.TenantResolver.
func TestCallTenant_LegacyNilResolver(t *testing.T) {
	ctx, got, ok := callTenant(context.Background(), nil)
	if !ok {
		t.Fatal("nil resolver must never deny")
	}
	if got != tenant.From(context.Background()) {
		t.Fatalf("nil resolver must return tenant.From(ctx), got %+v", got)
	}
	if ctx != context.Background() {
		t.Fatal("nil resolver must not stamp ctx")
	}
}

// TestCallTenant_ResolverPinAndDeny covers the multi-account seam: a resolver
// returning (t,true) pins t AND stamps it on ctx (so downstream tenant.From
// readers — ListQuery.Tenant, the detailer request — all agree); (_,false)
// denies fail-closed.
func TestCallTenant_ResolverPinAndDeny(t *testing.T) {
	pin := tenant.Tenant{CitySlug: "acct-uuid-1"}
	ctx, got, ok := callTenant(context.Background(), func(context.Context) (tenant.Tenant, bool) {
		return pin, true
	})
	if !ok || got != pin {
		t.Fatalf("expected pinned tenant, got %+v ok=%v", got, ok)
	}
	if tenant.From(ctx) != pin {
		t.Fatalf("resolved tenant must be stamped on ctx, got %+v", tenant.From(ctx))
	}

	if _, _, ok := callTenant(context.Background(), func(context.Context) (tenant.Tenant, bool) {
		return tenant.Tenant{}, false
	}); ok {
		t.Fatal("resolver (_,false) must deny")
	}
}

// TestListTool_TenantResolverPinned drives a real {resource}_list tool call over
// the in-memory transport with a pinned resolver and asserts the Lister sees the
// pinned tenant — the seam go-job wires to pin tenant=account from TokenInfo.
func TestListTool_TenantResolverPinned(t *testing.T) {
	var gotSlug string
	res := stubResource("acct")
	res.Lister = func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		gotSlug = q.Tenant.CitySlug
		return []resource.Row{{ID: "1"}}, 1, nil
	}
	cs := connectTools(t, []resource.Resource{res}, func(context.Context) (tenant.Tenant, bool) {
		return tenant.Tenant{CitySlug: "acct-uuid-1"}, true
	})
	out, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "acct_list"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool returned error: %+v", out)
	}
	if gotSlug != "acct-uuid-1" {
		t.Fatalf("lister must see pinned tenant, got %q", gotSlug)
	}
}

// TestListTool_TenantResolverDenies drives a tool call whose resolver denies —
// the tool must return an error result (fail-closed), never the global default.
func TestListTool_TenantResolverDenies(t *testing.T) {
	called := false
	res := stubResource("acct")
	res.Lister = func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
		called = true
		return nil, 0, nil
	}
	cs := connectTools(t, []resource.Resource{res}, func(context.Context) (tenant.Tenant, bool) {
		return tenant.Tenant{}, false
	})
	out, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "acct_list"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !out.IsError {
		t.Fatal("denied resolver must yield an error result, not a silent allow")
	}
	if called {
		t.Fatal("denied resolver must not reach the Lister")
	}
}

// TestGetTool_TenantResolverPinsDetailer verifies the pinned tenant reaches the
// Detailer through BOTH ctx and the request context — go-job's account-scoped
// detailers read tenant.From off one or the other.
func TestGetTool_TenantResolverPinsDetailer(t *testing.T) {
	var ctxSlug, reqSlug string
	res := stubResource("acct")
	res.Detailer = func(ctx context.Context, r *http.Request, _ string) ([]resource.DetailSection, error) {
		ctxSlug = tenant.From(ctx).CitySlug
		reqSlug = tenant.From(r.Context()).CitySlug
		return []resource.DetailSection{{Title: "ok"}}, nil
	}
	cs := connectTools(t, []resource.Resource{res}, func(context.Context) (tenant.Tenant, bool) {
		return tenant.Tenant{CitySlug: "acct-uuid-9"}, true
	})
	out, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "acct_get", Arguments: map[string]any{"id": "1"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool returned error: %+v", out)
	}
	if ctxSlug != "acct-uuid-9" || reqSlug != "acct-uuid-9" {
		t.Fatalf("detailer must see pinned tenant on ctx and request, got ctx=%q req=%q", ctxSlug, reqSlug)
	}
}
