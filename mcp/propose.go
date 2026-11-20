package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxProposeBytes caps the total size of the values one proposal may carry,
// matching the HTTP save route's 1 MB body limit.
const maxProposeBytes = 1 << 20

// validateWriteConfig checks Config.WriteResources against the exposed set.
func validateWriteConfig(exposed []resource.Resource, cfg Config) error {
	if len(cfg.WriteResources) == 0 {
		return nil
	}
	if strings.TrimSpace(cfg.ProposeNotice) == "" {
		return errors.New("panelmcp: Config.ProposeNotice is required when Config.WriteResources is set")
	}
	for _, name := range cfg.WriteResources {
		i := slices.IndexFunc(exposed, func(r resource.Resource) bool { return r.Name == name })
		if i < 0 {
			return fmt.Errorf("panelmcp: Config.WriteResources names %q, which is not an exposed Resource", name)
		}
		if exposed[i].Writer == nil {
			return fmt.Errorf("panelmcp: Config.WriteResources names %q, which has no Writer", name)
		}
	}
	return nil
}

type proposeInput struct {
	ID     string            `json:"id,omitempty" jsonschema:"row id to edit; omit to propose a new row"`
	Values map[string]string `json:"values" jsonschema:"field key to value; send every field of the form, an omitted field is treated as left blank"`
}

type proposeOutput struct {
	Resource string            `json:"resource"`
	ID       string            `json:"id,omitempty"`
	Accepted bool              `json:"accepted"`
	Errors   map[string]string `json:"errors,omitempty"`
	Notice   string            `json:"notice,omitempty"`
}

// registerProposeTools adds {resource}_propose for each name in writes. The
// gates mirror the read tools: a role-gated resource needs the RoleResolver, the
// tenant comes from the TenantResolver (a denial is fail-closed), and the save
// itself is Panel.Propose, the admin form's validation and Writer.Save.
func registerProposeTools(server *mcp.Server, p *resource.Panel, resources []resource.Resource, writes []string, notice string, logger *slog.Logger, tr func(context.Context) (tenant.Tenant, bool), roles roleFunc) {
	for _, r := range resources {
		if !slices.Contains(writes, r.Name) || r.Writer == nil {
			continue
		}
		if r.RequiredRole != "" && roles == nil {
			continue
		}
		registerProposeTool(server, p, r, notice, logger, tr, roles)
	}
}

func registerProposeTool(server *mcp.Server, p *resource.Panel, r resource.Resource, notice string, logger *slog.Logger, tr func(context.Context) (tenant.Tenant, bool), roles roleFunc) {
	toolName := r.Name + "_propose"
	tool := &mcp.Tool{
		Name:        toolName,
		Description: proposeDescription(r, notice),
	}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in proposeInput) (*mcp.CallToolResult, proposeOutput, error) {
		if !roleAllowed(ctx, r, roles) {
			return nil, proposeOutput{}, fmt.Errorf("%s: role denied", toolName)
		}
		size := 0
		for k, v := range in.Values {
			size += len(k) + len(v)
		}
		if size > maxProposeBytes {
			return nil, proposeOutput{}, fmt.Errorf("%s: values exceed %d bytes", toolName, maxProposeBytes)
		}
		// Unlike the read tools, a write never falls back to the fail-open
		// global tenant: with no resolver it is denied.
		if tr == nil {
			return nil, proposeOutput{}, fmt.Errorf("%s: tenant resolution denied", toolName)
		}
		pctx, tenantVal, ok := callTenant(ctx, tr)
		if !ok {
			return nil, proposeOutput{}, fmt.Errorf("%s: tenant resolution denied", toolName)
		}
		res, err := p.Propose(pctx, r, tenantVal, in.ID, in.Values)
		if err != nil {
			if errors.Is(err, resource.ErrDetailNotFound) {
				return nil, proposeOutput{}, fmt.Errorf("%s: row %q not found", toolName, in.ID)
			}
			return nil, proposeOutput{}, fmt.Errorf("%s: proposal failed", toolName)
		}
		out := proposeOutput{Resource: r.Name, ID: in.ID, Accepted: res.Accepted(), Errors: res.Errors}
		if out.Accepted {
			out.Notice = notice
		}
		return nil, out, nil
	})
	logger.Info("mcp tool registered", "tool", toolName, "resource", r.Name)
}

// proposeDescription tells the agent what it is calling: the form's fields,
// that values are validated like the admin form, and the deployment's own
// statement of what a proposal does.
func proposeDescription(r resource.Resource, notice string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Propose a change to %s: create a row when id is empty, otherwise edit row id. ", r.Title)
	b.WriteString("Values are validated like the admin form; send every field. ")
	fmt.Fprintf(&b, "A proposal is not a confirmed change. %s Fields: ", strings.TrimSpace(notice))
	for i, f := range r.Writer.Form.Fields {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Key)
		if f.Required {
			b.WriteString(" (required)")
		}
		if f.Kind == resource.FieldSelect && len(f.Options) > 0 {
			vals := make([]string, 0, len(f.Options))
			for _, o := range f.Options {
				vals = append(vals, o.Value)
			}
			fmt.Fprintf(&b, " (one of: %s)", strings.Join(vals, ", "))
		}
	}
	b.WriteString(".")
	return b.String()
}
