package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type savedCall struct {
	tenant string
	id     string
	values map[string]string
}

// writableResource builds a resource whose Save records its calls.
func writableResource(name string, saved *[]savedCall) resource.Resource {
	r := stubResource(name)
	r.Writer = &resource.Writer{
		Form: resource.FormSpec{Fields: []resource.Field{
			{Key: "title", Label: "Title", Kind: resource.FieldText, Required: true},
			{Key: "mode", Label: "Mode", Kind: resource.FieldSelect, Options: []resource.Option{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}},
			{Key: "on", Label: "On", Kind: resource.FieldCheckbox},
		}},
		Save: func(_ context.Context, t tenant.Tenant, id string, v map[string]string) error {
			*saved = append(*saved, savedCall{t.CitySlug, id, v})
			return nil
		},
	}
	return r
}

// connectPanel serves a real Panel's propose tools over the in-memory
// transport, with the same registration path Run uses.
func connectPanel(t *testing.T, writes []string, notice string, tr func(context.Context) (tenant.Tenant, bool), roles roleFunc, rs ...resource.Resource) *sdk.ClientSession {
	t.Helper()
	p := newPanel(t, rs...)
	server := sdk.NewServer(&sdk.Implementation{Name: "test", Version: "v0"}, nil)
	registerResourceTools(server, p.Resources(), slogDiscard(), tr, roles)
	registerProposeTools(server, p, p.Resources(), writes, notice, slogDiscard(), tr, roles)
	ct, st := sdk.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "c", Version: "v0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callPropose(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (*sdk.CallToolResult, proposeOutput) {
	t.Helper()
	out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var po proposeOutput
	if !out.IsError {
		sc, ok := out.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("%s: no structured content: %+v", name, out)
		}
		po.Accepted, _ = sc["accepted"].(bool)
		po.Notice, _ = sc["notice"].(string)
		if e, ok := sc["errors"].(map[string]any); ok {
			po.Errors = map[string]string{}
			for k, v := range e {
				po.Errors[k], _ = v.(string)
			}
		}
	}
	return out, po
}

const notice = "Nothing applies until the owner confirms."

func TestProposeTool_OptInOnly(t *testing.T) {
	var saved []savedCall
	cs := connectPanel(t, nil, "", pinTenant("a"), nil, writableResource("w", &saved))
	if has(toolNames(t, cs), "w_propose") {
		t.Fatal("no WriteResources: the surface must stay read-only")
	}
	cs = connectPanel(t, []string{"w"}, notice, pinTenant("a"), nil, writableResource("w", &saved), writableResource("other", &saved))
	names := toolNames(t, cs)
	if !has(names, "w_propose") || has(names, "other_propose") {
		t.Fatalf("exactly the opted-in resource gets a propose tool, got %v", names)
	}
}

func TestProposeTool_ValidationRunsBeforeSave(t *testing.T) {
	var saved []savedCall
	cs := connectPanel(t, []string{"w"}, notice, pinTenant("a"), nil, writableResource("w", &saved))

	// Required field empty, select outside its whitelist: rejected, Save never runs.
	_, po := callPropose(t, cs, "w_propose", map[string]any{"values": map[string]any{"title": "", "mode": "zzz"}})
	if po.Accepted || po.Errors["title"] == "" || po.Errors["mode"] == "" {
		t.Fatalf("invalid values must be rejected with per-field errors, got %+v", po)
	}
	if len(saved) != 0 {
		t.Fatalf("Writer.Save ran on invalid values: %+v", saved)
	}
	if po.Notice != "" {
		t.Fatalf("a rejected proposal must not carry the accepted notice, got %q", po.Notice)
	}
}

func TestProposeTool_AcceptedCallsSaveAsHTTPDoes(t *testing.T) {
	var saved []savedCall
	cs := connectPanel(t, []string{"w"}, notice, pinTenant("acct-1"), nil, writableResource("w", &saved))

	out, po := callPropose(t, cs, "w_propose", map[string]any{"id": "7", "values": map[string]any{"title": "x", "mode": "a", "on": "1", "junk": "ignored"}})
	if out.IsError || !po.Accepted || po.Notice != notice {
		t.Fatalf("valid proposal: accepted=%v notice=%q err=%v", po.Accepted, po.Notice, out.IsError)
	}
	if len(saved) != 1 {
		t.Fatalf("want one Save, got %d", len(saved))
	}
	got := saved[0]
	if got.tenant != "acct-1" || got.id != "7" {
		t.Errorf("Save saw tenant=%q id=%q, want the resolved tenant and the id", got.tenant, got.id)
	}
	// Checkbox normalised exactly like the HTTP collector; unknown keys dropped.
	if got.values["on"] != "true" || got.values["title"] != "x" {
		t.Errorf("Save values = %v", got.values)
	}
	if _, ok := got.values["junk"]; ok {
		t.Errorf("a key outside the form must not reach Save: %v", got.values)
	}

	// Create: empty id reaches Save as "".
	callPropose(t, cs, "w_propose", map[string]any{"values": map[string]any{"title": "new"}})
	if saved[1].id != "" {
		t.Errorf("create must reach Save with id \"\", got %q", saved[1].id)
	}
}

func TestProposeTool_SaveErrorIsAFieldErrorNotAnInternalLeak(t *testing.T) {
	var saved []savedCall
	r := writableResource("w", &saved)
	r.Writer.Save = func(context.Context, tenant.Tenant, string, map[string]string) error {
		return &resource.SaveError{Field: "title", Message: "already taken"}
	}
	cs := connectPanel(t, []string{"w"}, notice, pinTenant("a"), nil, r)
	_, po := callPropose(t, cs, "w_propose", map[string]any{"values": map[string]any{"title": "x"}})
	if po.Accepted || po.Errors["title"] != "already taken" {
		t.Fatalf("SaveError must surface as a field error, got %+v", po)
	}

	r.Writer.Save = func(context.Context, tenant.Tenant, string, map[string]string) error {
		return errors.New("pq: password authentication failed for user secret_db_user")
	}
	cs = connectPanel(t, []string{"w"}, notice, pinTenant("a"), nil, r)
	out, _ := callPropose(t, cs, "w_propose", map[string]any{"values": map[string]any{"title": "x"}})
	if !out.IsError {
		t.Fatal("an internal save failure must be an error result")
	}
	if txt := textOf(out); strings.Contains(txt, "secret_db_user") || strings.Contains(txt, "pq:") {
		t.Fatalf("internal error detail leaked to the agent: %q", txt)
	}
}

func textOf(out *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range out.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestProposeTool_TenantDenyAndNoResolverFailClosed(t *testing.T) {
	var saved []savedCall
	deny := func(context.Context) (tenant.Tenant, bool) { return tenant.Tenant{}, false }
	for name, tr := range map[string]func(context.Context) (tenant.Tenant, bool){"denying resolver": deny, "no resolver": nil} {
		cs := connectPanel(t, []string{"w"}, notice, tr, nil, writableResource("w", &saved))
		out, _ := callPropose(t, cs, "w_propose", map[string]any{"values": map[string]any{"title": "x"}})
		if !out.IsError {
			t.Errorf("%s: want an error result", name)
		}
	}
	if len(saved) != 0 {
		t.Fatalf("a denied tenant must never reach Save: %+v", saved)
	}
}

func TestProposeTool_RoleGate(t *testing.T) {
	var saved []savedCall
	gated := writableResource("w", &saved)
	gated.RequiredRole = "admin"

	cs := connectPanel(t, []string{"w"}, notice, pinTenant("a"), nil, gated)
	if has(toolNames(t, cs), "w_propose") {
		t.Fatal("role-gated resource exposed a propose tool without a RoleResolver")
	}
	cs = connectPanel(t, []string{"w"}, notice, pinTenant("a"), func(context.Context, string) bool { return false }, gated)
	out, _ := callPropose(t, cs, "w_propose", map[string]any{"values": map[string]any{"title": "x"}})
	if !out.IsError || len(saved) != 0 {
		t.Fatalf("a denying RoleResolver must block the write: err=%v saves=%d", out.IsError, len(saved))
	}
}

func TestProposeTool_DescriptionStatesNotice(t *testing.T) {
	var saved []savedCall
	cs := connectPanel(t, []string{"w"}, notice, pinTenant("a"), nil, writableResource("w", &saved))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range res.Tools {
		if tl.Name != "w_propose" {
			continue
		}
		for _, want := range []string{notice, "not a confirmed change", "title (required)", "one of: a, b"} {
			if !strings.Contains(tl.Description, want) {
				t.Errorf("description missing %q: %s", want, tl.Description)
			}
		}
		return
	}
	t.Fatal("w_propose not listed")
}

func TestValidateWriteConfig(t *testing.T) {
	var saved []savedCall
	w := writableResource("w", &saved)
	ro := stubResource("ro")
	ok := Config{WriteResources: []string{"w"}, ProposeNotice: notice}
	for name, tc := range map[string]struct {
		cfg     Config
		wantErr string
	}{
		"read-only default":  {Config{}, ""},
		"valid":              {ok, ""},
		"missing notice":     {Config{WriteResources: []string{"w"}}, "ProposeNotice"},
		"unknown resource":   {Config{WriteResources: []string{"nope"}, ProposeNotice: notice}, `"nope"`},
		"resource no writer": {Config{WriteResources: []string{"ro"}, ProposeNotice: notice}, "no Writer"},
	} {
		err := validateWriteConfig([]resource.Resource{w, ro}, tc.cfg)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: want error containing %q, got %v", name, tc.wantErr, err)
		}
	}
}

// --- list filters ---

func filterResource(name string) resource.Resource {
	r := stubResource(name)
	r.Filter = admintable.FilterSpec{Filters: []admintable.Filter{
		{Key: "status", SQLExpr: "status", Match: admintable.Eq, Allowed: []string{"open", "closed"}},
	}}
	return r
}

func TestListTool_FiltersReachListerAsWhere(t *testing.T) {
	res := filterResource("f")
	got := captureLister(&res)
	cs := connectTools(t, []resource.Resource{res}, nil)
	callList(t, cs, "f_list", map[string]any{"filters": map[string]any{"status": "open"}})
	if got.WhereConds != "status = $1" || len(got.WhereArgs) != 1 || got.WhereArgs[0] != "open" {
		t.Fatalf("filter must compose like the HTTP list: conds=%q args=%v", got.WhereConds, got.WhereArgs)
	}
}

func TestListTool_BadFilterIsAnErrorNotASilentNoop(t *testing.T) {
	res := filterResource("f")
	got := captureLister(&res)
	cs := connectTools(t, []resource.Resource{res}, nil)
	for name, f := range map[string]map[string]any{
		"undeclared key":  {"nope": "x"},
		"outside Allowed": {"status": "weird"},
	} {
		out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "f_list", Arguments: map[string]any{"filters": f}})
		if err != nil {
			t.Fatal(err)
		}
		if !out.IsError {
			t.Errorf("%s: want an error result", name)
		}
	}
	if got.WhereConds != "" {
		t.Fatalf("a rejected filter must not reach the Lister: %q", got.WhereConds)
	}
}

func TestListTool_FiltersAndScopeCompose(t *testing.T) {
	res := filterResource("f")
	res.Scope = tenant.Scope{Column: "t.city_slug"}
	got := captureLister(&res)
	cs := connectTools(t, []resource.Resource{res}, pinTenant("acct"))
	callList(t, cs, "f_list", map[string]any{"filters": map[string]any{"status": "closed"}})
	if got.WhereConds != "status = $1 AND t.city_slug = $2" || len(got.WhereArgs) != 2 || got.WhereArgs[1] != "acct" {
		t.Fatalf("scope clause must follow filter args: conds=%q args=%v", got.WhereConds, got.WhereArgs)
	}
}
