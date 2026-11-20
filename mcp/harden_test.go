package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-mcpserver"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubAuth is the smallest authenticator resource.New accepts; it also
// implements RoleAuthenticator so a role-gated resource can register.
type stubAuth struct{}

func (stubAuth) Require(h http.HandlerFunc) http.HandlerFunc { return h }
func (stubAuth) LoginHandler() http.Handler                  { return http.NotFoundHandler() }
func (stubAuth) LogoutHandler() http.Handler                 { return http.NotFoundHandler() }
func (stubAuth) RequireRole(_ string, h http.HandlerFunc) http.HandlerFunc {
	return h
}
func (stubAuth) HasRole(context.Context, string) bool { return true }

func newPanel(t *testing.T, rs ...resource.Resource) *resource.Panel {
	t.Helper()
	p := resource.New(resource.Config{Title: "T", BasePath: "/admin", Auth: stubAuth{}})
	for _, r := range rs {
		resource.Register(p, r)
	}
	return p
}

func slogDiscard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer func() { _ = l.Close() }()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func waitHealthy(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:" + port + "/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server on :%s never became healthy", port)
}

// startRun runs panelmcp.Run on a fresh port and returns the port plus a
// function that stops it and returns Run's error.
func startRun(t *testing.T, cfg Config) (port string, stop func() error) {
	t.Helper()
	port = freePort(t)
	cfg.Port = port
	cfg.Logger = slogDiscard()
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Context = ctx
	done := make(chan error, 1)
	go func() { done <- Run(cfg) }()
	waitHealthy(t, port)
	return port, func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			return errors.New("Run did not return after cancel")
		}
	}
}

func TestRun_NilBearerAuthRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	err := Run(Config{Panel: newPanel(t, stubResource("a")), Port: freePort(t), Context: ctx, Logger: slogDiscard()})
	if !errors.Is(err, ErrNoAuth) {
		t.Fatalf("Run with nil BearerAuth must return ErrNoAuth, got %v", err)
	}
}

func TestRun_AllowUnauthenticatedStarts(t *testing.T) {
	_, stop := startRun(t, Config{Panel: newPanel(t, stubResource("a")), AllowUnauthenticated: true})
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_UnknownResourceInAllowlist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	err := Run(Config{
		Panel: newPanel(t, stubResource("a")), Port: freePort(t), Context: ctx, Logger: slogDiscard(),
		AllowUnauthenticated: true, Resources: []string{"a", "typo"},
	})
	if err == nil || !strings.Contains(err.Error(), `"typo"`) {
		t.Fatalf("an unknown allowlist name must be a Run error naming it, got %v", err)
	}
}

// TestRun_SIGTERMStopsServer: a service manager stops containers with SIGTERM.
func TestRun_SIGTERMStopsServer(t *testing.T) {
	// Keep the test process alive if Run does NOT handle SIGTERM (a regression
	// must fail this test, not kill the test binary).
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	port := freePort(t)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Panel: newPanel(t, stubResource("a")), Port: port, AllowUnauthenticated: true, Logger: slogDiscard()})
	}()
	waitHealthy(t, port)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after SIGTERM: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after SIGTERM: signal not trapped")
	}
}

func postInitialize(t *testing.T, port, hostHeader string) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = hostHeader
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestRun_LoopbackDefaultKeepsRebindProtection: the default (loopback) bind
// rejects a foreign Host header; an explicit non-loopback Host (a container)
// opts out of that check, because its Host header is the proxy's business.
func TestRun_LoopbackDefaultKeepsRebindProtection(t *testing.T) {
	port, stop := startRun(t, Config{Panel: newPanel(t, stubResource("a")), AllowUnauthenticated: true})
	if got := postInitialize(t, port, "127.0.0.1:"+port); got != http.StatusOK {
		t.Errorf("loopback Host header: want 200, got %d", got)
	}
	if got := postInitialize(t, port, "evil.example"); got != http.StatusForbidden {
		t.Errorf("foreign Host header on the default loopback bind: want 403, got %d", got)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}

	port, stop = startRun(t, Config{Panel: newPanel(t, stubResource("a")), AllowUnauthenticated: true, Host: "0.0.0.0"})
	if got := postInitialize(t, port, "evil.example"); got == http.StatusForbidden {
		t.Errorf("explicit 0.0.0.0 bind must not apply the loopback Host check, got 403")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

// TestRun_DefaultBindsLoopbackOnly: the default listener is unreachable on a
// non-loopback interface address.
func TestRun_DefaultBindsLoopbackOnly(t *testing.T) {
	var ext string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			ext = ipn.IP.String()
			break
		}
	}
	if ext == "" {
		t.Skip("no non-loopback IPv4 interface on this host; bind default is covered by TestBindHost")
	}
	port, stop := startRun(t, Config{Panel: newPanel(t, stubResource("a")), AllowUnauthenticated: true})
	defer func() { _ = stop() }()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(ext, port), time.Second)
	if err == nil {
		_ = c.Close()
		t.Fatalf("default bind accepted a connection on %s:%s; want loopback only", ext, port)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "localhost": true, "127.1.2.3": true,
		"0.0.0.0": false, "": false, "10.0.0.5": false, "::": false, "example.com": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// --- tool-level: role, scope, clamp, columns, allowlist ---

func toolNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var out []string
	for _, tl := range res.Tools {
		out = append(out, tl.Name)
	}
	return out
}

func has(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

func TestRoleGatedResource_AbsentWithoutResolver(t *testing.T) {
	gated := stubResource("secret")
	gated.RequiredRole = "admin"
	cs := connectTools(t, []resource.Resource{stubResource("open"), gated}, nil)
	names := toolNames(t, cs)
	if !has(names, "open_list") {
		t.Fatalf("open resource missing from tools/list: %v", names)
	}
	if has(names, "secret_list") || has(names, "secret_get") {
		t.Fatalf("RequiredRole resource exposed without a RoleResolver: %v", names)
	}
}

func TestRoleGatedResource_ResolverDecidesEveryCall(t *testing.T) {
	called := 0
	gated := stubResource("secret")
	gated.RequiredRole = "admin"
	gated.Lister = func(context.Context, resource.ListQuery) ([]resource.Row, int, error) {
		called++
		return nil, 0, nil
	}
	allow := false
	var askedRole string
	cs := connectTools(t, []resource.Resource{gated}, nil, func(_ context.Context, role string) bool {
		askedRole = role
		return allow
	})
	if !has(toolNames(t, cs), "secret_list") {
		t.Fatal("gated resource must be listed when a RoleResolver is configured")
	}
	for _, tool := range []struct {
		name string
		args map[string]any
	}{{"secret_list", nil}, {"secret_get", map[string]any{"id": "1"}}} {
		out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tool.name, Arguments: tool.args})
		if err != nil {
			t.Fatalf("%s: %v", tool.name, err)
		}
		if !out.IsError {
			t.Fatalf("%s: a denying resolver must yield an error result", tool.name)
		}
	}
	if called != 0 {
		t.Fatal("denied role must not reach the Lister")
	}
	if askedRole != "admin" {
		t.Fatalf("resolver asked for role %q, want the resource's RequiredRole", askedRole)
	}
	allow = true
	out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "secret_list"})
	if err != nil || out.IsError {
		t.Fatalf("approved role must succeed: err=%v out=%+v", err, out)
	}
	if called != 1 {
		t.Fatalf("approved call must reach the Lister once, got %d", called)
	}
}

func TestSelectResources_AllowlistFiltersToolsList(t *testing.T) {
	all := []resource.Resource{stubResource("a"), stubResource("b"), stubResource("c")}
	got, err := selectResources(all, []string{"c", "a"})
	if err != nil {
		t.Fatal(err)
	}
	names := toolNames(t, connectTools(t, got, nil))
	if has(names, "b_list") || !has(names, "a_list") || !has(names, "c_list") {
		t.Fatalf("allowlist [a c] must expose exactly a and c, got %v", names)
	}
	if _, err := selectResources(all, nil); err != nil {
		t.Fatalf("empty allowlist keeps everything: %v", err)
	}
}

// captureLister records the last ListQuery the tool handed to the Lister.
func captureLister(res *resource.Resource) *resource.ListQuery {
	var last resource.ListQuery
	res.Lister = func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
		last = q
		return nil, 0, nil
	}
	return &last
}

func pinTenant(slug string) func(context.Context) (tenant.Tenant, bool) {
	return func(context.Context) (tenant.Tenant, bool) { return tenant.Tenant{CitySlug: slug}, true }
}

func callList(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) listOutput {
	t.Helper()
	out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out.IsError {
		t.Fatalf("%s returned an error result: %+v", name, out.Content)
	}
	raw, err := json.Marshal(out.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var lo listOutput
	if err := json.Unmarshal(raw, &lo); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return lo
}

func TestListTool_AppliesTenantScopeClause(t *testing.T) {
	scoped := stubResource("scoped")
	scoped.Scope = tenant.Scope{Column: "p.city_slug"}
	got := captureLister(&scoped)
	global := stubResource("global")
	gotGlobal := captureLister(&global)
	cs := connectTools(t, []resource.Resource{scoped, global}, pinTenant("tenant-a"))

	callList(t, cs, "scoped_list", nil)
	if got.WhereConds != "p.city_slug = $1" {
		t.Errorf("scoped list WhereConds = %q, want the scope clause %q", got.WhereConds, "p.city_slug = $1")
	}
	if len(got.WhereArgs) != 1 || got.WhereArgs[0] != "tenant-a" {
		t.Errorf("scoped list WhereArgs = %v, want [tenant-a]", got.WhereArgs)
	}

	callList(t, cs, "global_list", nil)
	if gotGlobal.WhereConds != "" || len(gotGlobal.WhereArgs) != 0 {
		t.Errorf("unscoped list must carry no WHERE, got %q %v", gotGlobal.WhereConds, gotGlobal.WhereArgs)
	}
}

func TestListTool_ClampsLimitAndOffset(t *testing.T) {
	res := stubResource("r")
	got := captureLister(&res)
	cs := connectTools(t, []resource.Resource{res}, nil)
	for _, tc := range []struct {
		name         string
		limit, off   int
		wantL, wantO int
	}{
		{"over cap", 100000, 0, 500, 0},
		{"at cap", 500, 0, 500, 0},
		{"default", 0, 0, 50, 0},
		{"negative limit", -5, 0, 50, 0},
		{"negative offset", 10, -3, 10, 0},
	} {
		lo := callList(t, cs, "r_list", map[string]any{"limit": tc.limit, "offset": tc.off})
		if got.Limit != tc.wantL || got.Offset != tc.wantO {
			t.Errorf("%s: Lister saw limit=%d offset=%d, want %d/%d", tc.name, got.Limit, got.Offset, tc.wantL, tc.wantO)
		}
		if lo.Limit != tc.wantL || lo.Offset != tc.wantO {
			t.Errorf("%s: output echoes limit=%d offset=%d, want %d/%d", tc.name, lo.Limit, lo.Offset, tc.wantL, tc.wantO)
		}
	}
}

func TestListTool_ReturnsColumns(t *testing.T) {
	res := stubResource("r")
	res.Sort.Columns = []admintable.Column{{Key: "id", Label: "ID", SQLExpr: "id", Sortable: true}, {Key: "name", Label: "Name", SQLExpr: "name"}}
	cs := connectTools(t, []resource.Resource{res}, nil)
	lo := callList(t, cs, "r_list", nil)
	want := []columnJSON{{"id", "ID"}, {"name", "Name"}}
	if fmt.Sprint(lo.Columns) != fmt.Sprint(want) {
		t.Fatalf("columns = %v, want %v", lo.Columns, want)
	}
}

// compile-time: the mcpserver import is part of Config's public shape.
var _ = (*mcpserver.BearerAuth)(nil)
