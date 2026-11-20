// Package mcp auto-exposes go-panel Resources as MCP tools.
//
// Each registered Resource becomes two MCP tools:
//
//   - {resource_name}_list — list rows with optional pagination and sort.
//     Parameters: limit (int, default 50, max 500), offset (int, default 0),
//     sort_key (string, optional — must be one of the Resource's sortable
//     column keys), sort_dir (string, "asc" or "desc", optional).
//     Returns a JSON object: {"columns": [{key, label}], "rows": [...], "total": N};
//     a row's cells follow the column order.
//
//   - {resource_name}_get — get a single row's detail by ID.
//     Only registered when the Resource has a Detailer.
//     Parameters: id (string, required).
//     Returns a JSON array of DetailSection objects.
//
// The MCP server runs on a separate port from the admin panel HTTP server,
// sharing the same Panel instance and its registered Resources.
//
// # Secure by default
//
//   - Authentication is required: Run returns an error when Config.BearerAuth
//     is nil unless Config.AllowUnauthenticated is set explicitly.
//   - The listener binds the loopback interface (Config.Host defaults to
//     127.0.0.1) and the SDK's Host-header (DNS rebinding) protection is on.
//     Binding a non-loopback Host turns that protection off, since a reverse
//     proxy or container network then legitimately forwards foreign Host
//     headers.
//   - A Resource with a RequiredRole is not exposed unless Config.RoleResolver
//     is set, and is then checked on every call.
//   - Config.Resources, when set, is an allowlist of exposed Resource names.
//   - A list call composes the same WHERE (declared filters plus the tenant
//     scope clause) as the HTTP list page, through resource.Resource.ListWhere.
//
// Tenant context comes from Config.TenantResolver; without one it is the
// global default (single-tenant).
//
// Usage:
//
//	panel := resource.New(cfg)
//	resource.Register(panel, myResource)
//	// ... register more resources ...
//	go func() {
//	    if err := panelmcp.Run(panelmcp.Config{
//	        Panel:      panel,
//	        Port:       "8895",
//	        BearerAuth: &mcpserver.BearerAuth{Verifier: verifier},
//	    }); err != nil {
//	        slog.Error("mcp server failed", "error", err)
//	    }
//	}()
//	// serve panel.Handler() on the admin port as usual
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"syscall"

	"github.com/anatolykoptev/go-mcpserver"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultListLimit = 50
	maxListLimit     = 500
)

// Config configures the MCP server that exposes panel Resources as MCP tools.
type Config struct {
	// Panel is the admin panel whose Resources are exposed as MCP tools.
	// Required — Run panics if nil.
	Panel *resource.Panel

	// Port is the MCP server port. Defaults to the MCP_PORT env var or "8090".
	Port string

	// Host is the interface to bind. Empty = "127.0.0.1" (loopback only). A
	// container whose published port must reach the listener sets "0.0.0.0"
	// (or a specific address) explicitly; that also disables the SDK's
	// loopback Host-header protection, which only makes sense for a loopback
	// bind.
	Host string
	// BearerAuth gates /mcp. Required: Run returns an error when it is nil
	// unless AllowUnauthenticated is set.
	BearerAuth *mcpserver.BearerAuth
	// AllowUnauthenticated is the explicit opt-out of BearerAuth, for a
	// loopback-only deployment whose only caller is local. Ignored when
	// BearerAuth is set. Run logs a warning when it takes effect.
	AllowUnauthenticated bool
	// Resources, when non-empty, is an allowlist of Resource names to expose.
	// nil/empty = every registered Resource (subject to RoleResolver for role-
	// gated ones). A name that is not a registered Resource is a Run error, so
	// a typo cannot silently expose less (or nothing) than intended.
	Resources []string
	// RoleResolver decides whether the caller identified by ctx (typically the
	// verified bearer identity) holds a role. A Resource with a non-empty
	// RequiredRole is exposed only when this is set, and every call to it is
	// denied unless RoleResolver(ctx, RequiredRole) is true. The HTTP "owner"
	// super-role is the resolver's business: return true for it if it applies.
	// nil = role-gated Resources are not exposed over MCP at all.
	RoleResolver func(ctx context.Context, role string) bool

	// TenantResolver, when non-nil, resolves the tenant for EVERY tool call
	// from the call's ctx — the seam a multi-account consumer uses to pin the
	// tenant from the verified bearer identity (mcpserver.TokenInfoFromContext)
	// instead of the fail-open global default that tenant.From(ctx) returns.
	// A resolver returning (zero Tenant, false) DENIES the call fail-closed.
	//
	// nil preserves the legacy single-tenant behavior: tenant.From(ctx), which
	// is the global default whenever nothing stamped a tenant. The tools also
	// stamp the resolved tenant back onto the ctx handed to Listers/Detailers,
	// so detailers reading tenant.From(req.Context()) see the pinned value.
	TenantResolver func(ctx context.Context) (tenant.Tenant, bool)

	// Logger. nil = slog.Default().
	Logger *slog.Logger

	// Context for lifecycle. nil = signal.NotifyContext(SIGINT, SIGTERM).
	Context context.Context
}

// ErrNoAuth is returned by Run when Config.BearerAuth is nil and
// Config.AllowUnauthenticated is not set.
var ErrNoAuth = errors.New("panelmcp: Config.BearerAuth is required (set Config.AllowUnauthenticated to run without authentication)")

// defaultHost is the bind address when Config.Host is empty.
const defaultHost = "127.0.0.1"

// isLoopbackHost reports whether host names the loopback interface only.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Run starts the MCP server, exposing the selected registered Resources as MCP
// tools. Blocks until the context is cancelled or SIGINT/SIGTERM is received.
// It returns ErrNoAuth for a nil BearerAuth without AllowUnauthenticated.
func Run(cfg Config) error {
	if cfg.Panel == nil {
		panic("panelmcp.Run: Config.Panel is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.BearerAuth == nil {
		if !cfg.AllowUnauthenticated {
			return ErrNoAuth
		}
		logger.Warn("panelmcp: running WITHOUT authentication (Config.AllowUnauthenticated)")
	}
	exposed, err := selectResources(cfg.Panel.Resources(), cfg.Resources)
	if err != nil {
		return err
	}
	host := cfg.Host
	if host == "" {
		host = defaultHost
	}
	ctx := cfg.Context
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
	}

	// Stateless on purpose: GET /mcp answers 405 + Allow: POST, which rmcp reads as
	// "no standalone stream". The 2026-10-08 stateful flip assumed that would stop
	// an SSE error loop; the premise was disproved and it only added session expiry.
	mcpCfg := mcpserver.Config{
		Name:                       "go-panel",
		Version:                    moduleVersion(),
		Host:                       host,
		Port:                       cfg.Port,
		SchemaCache:                mcp.NewSchemaCache(),
		DisableLocalhostProtection: !isLoopbackHost(host),
		Context:                    ctx,
		Logger:                     logger,
		MCPLogger:                  logger,
		BearerAuth:                 cfg.BearerAuth,
		JSONResponse:               true,
	}
	return mcpserver.Serve(&mcp.Implementation{
		Name:    "go-panel",
		Version: moduleVersion(),
	}, mcpCfg, func(s *mcp.Server) {
		registerResourceTools(s, exposed, logger, cfg.TenantResolver, cfg.RoleResolver)
	})
}

// selectResources applies the Config.Resources allowlist. An empty allowlist
// keeps every resource; an unknown name is an error.
func selectResources(all []resource.Resource, allow []string) ([]resource.Resource, error) {
	if len(allow) == 0 {
		return all, nil
	}
	for _, name := range allow {
		if !slices.ContainsFunc(all, func(r resource.Resource) bool { return r.Name == name }) {
			return nil, fmt.Errorf("panelmcp: Config.Resources names %q, which is not a registered Resource", name)
		}
	}
	out := make([]resource.Resource, 0, len(allow))
	for _, r := range all {
		if slices.Contains(allow, r.Name) {
			out = append(out, r)
		}
	}
	return out, nil
}

// modulePath is go-panel's own import path — the key its version is filed
// under in a consumer's build info.
const modulePath = "github.com/anatolykoptev/go-panel"

// moduleVersion reports the go-panel release a binary was built against, read
// out of the build info the toolchain stamps from the go.mod requirement.
//
// It exists because the two version fields below it used to hold the literal
// "0.1.0". Nothing referenced that number, so nothing ever went red when it
// drifted, and by v0.22.2 every MCP client had been told go-panel was at 0.1.0
// for twenty-two releases. release-please owns the version; a second copy
// maintained by hand can only ever be wrong.
//
// Three cases, in the order they occur:
//   - go-panel is a dependency: the version comes from Deps, or from Replace
//     when a consumer points the requirement elsewhere (a replace directive is
//     what actually gets built, so it is what gets reported).
//   - go-panel is the main module — its own tests, or `go run ./...` — where
//     the toolchain has no released version to stamp. "(devel)" is the
//     toolchain's own word for that, not an error.
//   - no build info at all, which a normally-built binary never hits.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	for _, d := range bi.Deps {
		if d.Path != modulePath {
			continue
		}
		if d.Replace != nil && d.Replace.Version != "" {
			return d.Replace.Version
		}
		if d.Version != "" {
			return d.Version
		}
	}
	if bi.Main.Path == modulePath && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}

// roleFunc is Config.RoleResolver's shape.
type roleFunc = func(ctx context.Context, role string) bool

// roleAllowed is the per-call role gate. A Resource without RequiredRole is
// open to any authenticated caller; a role-gated one needs a resolver AND its
// approval (registration already skips it when there is no resolver, this is
// the call-time half so a revoked role takes effect without a restart).
func roleAllowed(ctx context.Context, r resource.Resource, roles roleFunc) bool {
	if r.RequiredRole == "" {
		return true
	}
	return roles != nil && roles(ctx, r.RequiredRole)
}

// registerResourceTools creates MCP list/get tools for each Resource. A
// Resource with a RequiredRole is skipped unless roles is non-nil.
func registerResourceTools(server *mcp.Server, resources []resource.Resource, logger *slog.Logger, tr func(context.Context) (tenant.Tenant, bool), roles roleFunc) {
	for _, r := range resources {
		if r.RequiredRole != "" && roles == nil {
			logger.Info("mcp resource not exposed: RequiredRole set and no RoleResolver", "resource", r.Name)
			continue
		}
		registerListTool(server, r, logger, tr, roles)
		// EffectiveDetailer returns the hand-written Detailer OR a synthesized
		// auto-Detailer built from Sort.Columns + FetchRow. This keeps MCP's
		// {resource}_get tool in sync with the HTTP detail route (which is
		// mounted whenever Detailer OR FetchRow is non-nil — see resource.Register).
		if resource.EffectiveDetailer(r) != nil {
			registerGetTool(server, r, logger, tr, roles)
		}
	}
}

// callTenant resolves the tenant for one tool call. A nil resolver keeps the
// legacy behavior — tenant.From(ctx), which fails OPEN to the global default
// (fine for the single-tenant deployments panelmcp was built for). A set
// resolver is authoritative: (tenant, true) pins, (_, false) denies fail-closed.
// On a successful resolve the ctx is stamped (tenant.WithTenant) so both the
// Lister's ListQuery.Tenant and anything reading tenant.From(ctx) downstream —
// the detailer's request context included — observe the SAME pinned tenant.
func callTenant(ctx context.Context, tr func(context.Context) (tenant.Tenant, bool)) (context.Context, tenant.Tenant, bool) {
	if tr == nil {
		return ctx, tenant.From(ctx), true
	}
	t, ok := tr(ctx)
	if !ok {
		return ctx, tenant.Tenant{}, false
	}
	return tenant.WithTenant(ctx, t), t, true
}

// --- list tool ---

type listInput struct {
	Limit   int    `json:"limit,omitempty" jsonschema:"max 500 rows per page; default 50"`
	Offset  int    `json:"offset,omitempty" jsonschema:"zero-based offset"`
	SortKey string `json:"sort_key,omitempty" jsonschema:"field name to sort by"`
	SortDir string `json:"sort_dir,omitempty" jsonschema:"asc or desc"`
}

type columnJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type listOutput struct {
	Resource string `json:"resource"`
	// Columns names the cells of every row, in order: Rows[i].Cells[j] is the
	// value of Columns[j].
	Columns []columnJSON `json:"columns"`
	Rows    []rowJSON    `json:"rows"`
	Total   int          `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
}

type rowJSON struct {
	ID    string     `json:"id"`
	Cells []cellJSON `json:"cells"`
	Href  string     `json:"href,omitempty"`
}

type cellJSON struct {
	Value string `json:"value"`
	HTML  bool   `json:"html"`
}

func registerListTool(server *mcp.Server, r resource.Resource, logger *slog.Logger, tr func(context.Context) (tenant.Tenant, bool), roles roleFunc) {
	toolName := r.Name + "_list"
	tool := &mcp.Tool{
		Name:        toolName,
		Description: fmt.Sprintf("List rows from the %s admin resource. Returns the column keys, rows with cell values (in column order) and the total count.", r.Title),
	}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput, error) {
		if !roleAllowed(ctx, r, roles) {
			return nil, listOutput{}, fmt.Errorf("%s: role denied", toolName)
		}
		limit := in.Limit
		if limit <= 0 {
			limit = defaultListLimit
		}
		if limit > maxListLimit {
			limit = maxListLimit
		}
		offset := in.Offset
		if offset < 0 {
			offset = 0
		}
		sortState := r.Sort.Resolve(in.SortKey, in.SortDir)
		lctx, tenantVal, ok := callTenant(ctx, tr)
		if !ok {
			return nil, listOutput{}, fmt.Errorf("%s: tenant resolution denied", toolName)
		}
		// Same composition as the HTTP list page: tenant scope clause included.
		whereConds, whereArgs := r.ListWhere(nil, tenantVal)
		rows, total, err := r.Lister(lctx, resource.ListQuery{
			Sort:       sortState,
			WhereConds: whereConds,
			WhereArgs:  whereArgs,
			Tenant:     tenantVal,
			Limit:      limit,
			Offset:     offset,
		})
		if err != nil {
			return nil, listOutput{}, fmt.Errorf("%s: list failed: %w", toolName, err)
		}
		return nil, listOutput{
			Resource: r.Name,
			Columns:  columnsToJSON(r),
			Rows:     rowsToJSON(rows),
			Total:    total,
			Limit:    limit,
			Offset:   offset,
		}, nil
	})
	logger.Info("mcp tool registered", "tool", toolName, "resource", r.Name)
}

// --- get tool ---

type getInput struct {
	ID string `json:"id" jsonschema:"required"`
}

type getOutput struct {
	Resource string        `json:"resource"`
	ID       string        `json:"id"`
	Sections []sectionJSON `json:"sections"`
}

type sectionJSON struct {
	Title   string     `json:"title,omitempty"`
	Items   []itemJSON `json:"items,omitempty"`
	RawHTML string     `json:"raw_html,omitempty"`
}

type itemJSON struct {
	Label string `json:"label"`
	Value string `json:"value"`
	HTML  bool   `json:"html"`
}

func registerGetTool(server *mcp.Server, r resource.Resource, logger *slog.Logger, tr func(context.Context) (tenant.Tenant, bool), roles roleFunc) {
	toolName := r.Name + "_get"
	tool := &mcp.Tool{
		Name:        toolName,
		Description: fmt.Sprintf("Get a single %s row's detail by ID. Returns detail sections with labeled items.", r.Title),
	}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in getInput) (*mcp.CallToolResult, getOutput, error) {
		if !roleAllowed(ctx, r, roles) {
			return nil, getOutput{}, fmt.Errorf("%s: role denied", toolName)
		}
		if in.ID == "" {
			return nil, getOutput{}, fmt.Errorf("%s: id is required", toolName)
		}
		dctx, tenantVal, ok := callTenant(ctx, tr)
		if !ok {
			return nil, getOutput{}, fmt.Errorf("%s: tenant resolution denied", toolName)
		}
		req, err := http.NewRequestWithContext(tenant.WithTenant(dctx, tenantVal), http.MethodGet, "/", nil)
		if err != nil {
			return nil, getOutput{}, fmt.Errorf("%s: internal request build failed: %w", toolName, err)
		}
		// EffectiveDetailer handles both hand-written Detailer and the
		// FetchRow-backed auto-Detailer (see resource.EffectiveDetailer). The
		// detailer receives dctx — with the resolved tenant stamped on it when a
		// TenantResolver is configured — so tenant.From works identically whether
		// it reads the ctx or the request.
		detailer := resource.EffectiveDetailer(r)
		sections, err := detailer(dctx, req, in.ID)
		if err != nil {
			return nil, getOutput{}, fmt.Errorf("%s: detail failed: %w", toolName, err)
		}
		return nil, getOutput{
			Resource: r.Name,
			ID:       in.ID,
			Sections: sectionsToJSON(sections),
		}, nil
	})
	logger.Info("mcp tool registered", "tool", toolName, "resource", r.Name)
}

// --- JSON helpers ---

func columnsToJSON(r resource.Resource) []columnJSON {
	out := make([]columnJSON, 0, len(r.Sort.Columns))
	for _, c := range r.Sort.Columns {
		out = append(out, columnJSON{Key: c.Key, Label: c.Label})
	}
	return out
}

func rowsToJSON(rows []resource.Row) []rowJSON {
	out := make([]rowJSON, 0, len(rows))
	for _, row := range rows {
		cells := make([]cellJSON, 0, len(row.Cells))
		for _, c := range row.Cells {
			cells = append(cells, cellJSON{Value: c.Value, HTML: c.HTML})
		}
		out = append(out, rowJSON{ID: row.ID, Cells: cells, Href: row.Href})
	}
	return out
}

func sectionsToJSON(sections []resource.DetailSection) []sectionJSON {
	out := make([]sectionJSON, 0, len(sections))
	for _, s := range sections {
		items := make([]itemJSON, 0, len(s.Items))
		for _, item := range s.Items {
			items = append(items, itemJSON{Label: item.Label, Value: item.Value, HTML: item.HTML})
		}
		out = append(out, sectionJSON{Title: s.Title, Items: items, RawHTML: s.RawHTML})
	}
	return out
}
