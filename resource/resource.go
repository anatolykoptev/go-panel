// Package resource is the core abstraction of go-panel.
//
// A consumer declares a Resource once and calls Register to get a working
// admin list page: sort headers, filter bar, pagination, nav entry — all
// generated from the declaration with zero hand-written table HTML.
//
// SQL-safety invariant: only Spec-owned SQLExpr + FilterSpec-owned SQLExpr
// values + literal operators reach SQL. URL bytes become bind args only.
// The Resource.Lister closure receives pre-composed WhereConds/WhereArgs
// from FilterSpec — it must never build additional WHERE from raw URL params.
//
// Tenant-scope invariant: every non-Global resource gets the city_slug WHERE
// clause injected unconditionally. A resource with an empty Scope = global
// (no scope injection). The fitness test in this package asserts this.
//
// Usage:
//
//	var placesRes = resource.Resource{
//	    Name:  "places",
//	    Title: "Places",
//	    Sort:  admintable.Spec{...},
//	    Filter: admintable.FilterSpec{...},
//	    Scope: tenant.Scope{Column: "p.city_slug"},
//	    Lister: func(ctx context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
//	        return store.ListPlaces(ctx, q)
//	    },
//	}
//	resource.Register(panel, placesRes)
package resource

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/render"
	"github.com/anatolykoptev/go-panel/shell"
	"github.com/anatolykoptev/go-panel/tenant"
)

const (
	defaultPageSize = 50
	maxPageSize     = 500
)

// Cell is one table cell value.
type Cell struct {
	Value string
	HTML  bool // when true, Value is rendered as raw HTML (must be trusted/escaped by caller)
}

// Row is one table row in a list response.
type Row struct {
	ID    string
	Cells []Cell // ordered to match Sort.Columns
	Href  string // optional detail link
}

// ListQuery is the safe, resolved query handed to the Lister closure.
// It contains only values safe to use in SQL: pre-composed WHERE fragment
// (from FilterSpec — only author SQLExpr + literal ops), bind args, resolved
// sort state, tenant, and paging.
//
// IMPORTANT: never build additional WHERE from the raw HTTP request inside
// a Lister. Use only WhereConds/WhereArgs + Tenant + Sort.
type ListQuery struct {
	Sort       admintable.State
	WhereConds string // from FilterSpec.Where — only author SQLExpr + literal ops
	WhereArgs  []any  // URL values as bind args; index from 1 = startArg passed to FilterSpec.Where
	Tenant     tenant.Tenant
	Limit      int
	Offset     int
}

// Perms declares who may read or write this resource.
// Foundations: ReadAny means any authenticated session can read.
// Write perms are relevant only from Phase 2 (form/edit).
type Perms struct {
	// ReadAny allows any authenticated operator to read. Default.
	ReadAny bool
}

// ReadAny is the foundations default: any authenticated operator can read.
var ReadAny = Perms{ReadAny: true}

// Resource is the declarative contract for one admin entity.
// Declare once, get list + (later) detail + edit + MCP for free.
//
// Compile-time invariant: Sort and Filter must be validated at startup via
// Register (panics on invalid Spec/FilterSpec). Never derive Sort.Columns
// SQLExpr or Filter.Filters SQLExpr from user input.
type Resource struct {
	Name   string // URL/nav slug, e.g. "places". Must be URL-safe.
	Title  string // human label, e.g. "Places"
	Icon   string // sidebar emoji/icon, e.g. "📍"
	Group  string // sidebar group label, e.g. "Content". Empty = no group.
	Sort   admintable.Spec
	Filter admintable.FilterSpec
	Scope  tenant.Scope // city_slug scope; empty = global
	Perms  Perms

	// Lister fetches one page of rows. The kit hands it a safe ListQuery;
	// the app owns the row type + scan. go-panel never assumes a schema.
	Lister func(ctx context.Context, q ListQuery) (rows []Row, total int, err error)

	// Writer enables create/edit forms. Nil = read-only (Phase 1 behaviour, default).
	// When non-nil, CSRFKey must be set in Config (panic at Register if missing — fail-closed).
	Writer *Writer
}

// Panel is the minimal composition root go-panel provides.
// It holds the mux, authenticator, tenant resolver, and the registered nav.
// Consumers create it via New() and call Handler() to get the http.Handler.
type Panel struct {
	mux  *http.ServeMux
	auth interface {
		Require(http.HandlerFunc) http.HandlerFunc
		LoginHandler() http.Handler
		LogoutHandler() http.Handler
	}
	resolver tenant.Resolver
	basePath string
	nav      []shell.NavItem
	title    string
	csrfKey  []byte
}

// sessionCookier is the optional interface implemented by authenticators that
// expose their session cookie name, used for CSRF double-submit binding.
type sessionCookier interface {
	SessionCookieName() string
}

// Config holds Panel configuration.
type Config struct {
	Title    string
	BasePath string // e.g. "/admin". Defaults to "/admin".
	Auth     interface {
		Require(http.HandlerFunc) http.HandlerFunc
		LoginHandler() http.Handler
		LogoutHandler() http.Handler
	}
	Resolver tenant.Resolver // nil = PathResolver{Segment:2}
	// CSRFKey is the HMAC signing key for CSRF double-submit tokens.
	// Required when any Resource has a non-nil Writer; omitting it causes
	// a panic at Register time (fail-closed configuration).
	CSRFKey []byte
}

// New creates a Panel with the given config.
func New(cfg Config) *Panel {
	bp := cfg.BasePath
	if bp == "" {
		bp = "/admin"
	}
	title := cfg.Title
	if title == "" {
		title = "Admin"
	}
	resolver := cfg.Resolver
	if resolver == nil {
		resolver = tenant.PathResolver{Segment: 2}
	}
	p := &Panel{
		mux:      http.NewServeMux(),
		auth:     cfg.Auth,
		resolver: resolver,
		basePath: bp,
		title:    title,
		csrfKey:  cfg.CSRFKey,
	}
	// Mount standard routes.
	p.mux.Handle(bp+"/static/", http.StripPrefix(bp+"/static", shell.StaticHandler()))
	p.mux.Handle(bp+"/login", cfg.Auth.LoginHandler())
	p.mux.Handle(bp+"/logout", cfg.Auth.LogoutHandler())
	return p
}

// Handler returns the http.Handler for the entire admin surface.
// Mount at the admin path (e.g. /admin/) in your app mux.
func (p *Panel) Handler() http.Handler {
	return p.mux
}

// NavItems returns a snapshot of registered nav items (for testing/introspection).
func (p *Panel) NavItems() []shell.NavItem {
	result := make([]shell.NavItem, len(p.nav))
	copy(result, p.nav)
	return result
}

// Register mounts the resource's list handler and adds it to the sidebar nav.
// Panics at startup if Sort or Filter are misconfigured (fail-fast, not at runtime).
// When the resource has a Writer, also panics if CSRFKey is empty (fail-closed).
//
// Mounted routes:
//
//	GET  {basePath}/{name}            — list page (full or htmx fragment)
//	GET  {basePath}/{name}/rows       — htmx row fragment only (sort/filter swap target)
//	GET  {basePath}/{name}/new        — empty create form (only when Writer != nil)
//	GET  {basePath}/{name}/{id}/edit  — pre-populated edit form (only when Writer != nil)
//	POST {basePath}/{name}/{id}/save  — save (id=="new" means create) (only when Writer != nil)
func Register(p *Panel, r Resource) {
	if err := r.Sort.Valid(); err != nil {
		panic(fmt.Sprintf("resource.Register %q: invalid Sort: %v", r.Name, err))
	}
	if err := r.Filter.Valid(); err != nil {
		panic(fmt.Sprintf("resource.Register %q: invalid Filter: %v", r.Name, err))
	}
	if r.Writer != nil {
		if len(p.csrfKey) == 0 {
			panic(fmt.Sprintf("resource.Register %q: Writer is set but Config.CSRFKey is empty — set CSRFKey to enable write forms (fail-closed)", r.Name))
		}
		if err := r.Writer.Form.Valid(); err != nil {
			panic(fmt.Sprintf("resource.Register %q: invalid Writer.Form: %v", r.Name, err))
		}
	}

	// Add nav entry.
	if r.Group != "" {
		// Insert group header if not already present.
		groupKey := "group:" + r.Group
		found := false
		for _, n := range p.nav {
			if n.Group == r.Group && n.ID == groupKey {
				found = true
				break
			}
		}
		if !found {
			p.nav = append(p.nav, shell.NavItem{
				ID:    groupKey,
				Group: r.Group,
			})
		}
	}
	p.nav = append(p.nav, shell.NavItem{
		ID:    r.Name,
		Label: r.Title,
		Icon:  r.Icon,
		URL:   p.basePath + "/" + r.Name,
	})

	listPath := p.basePath + "/" + r.Name
	rowsPath := p.basePath + "/" + r.Name + "/rows"

	listHandler := p.makeListHandler(r)
	p.mux.HandleFunc("GET "+listPath, p.auth.Require(func(w http.ResponseWriter, req *http.Request) {
		// Update nav active state.
		nav := p.activeNav(r.Name)
		shell.SecurityHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		listHandler(w, req, nav, false)
	}))
	p.mux.HandleFunc("GET "+rowsPath, p.auth.Require(func(w http.ResponseWriter, req *http.Request) {
		shell.SecurityHeaders(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		listHandler(w, req, nil, true)
	}))

	// Writer routes — only mounted when Writer is configured.
	if r.Writer != nil {
		newPath := p.basePath + "/" + r.Name + "/new"
		editPath := p.basePath + "/" + r.Name + "/{id}/edit"
		savePath := p.basePath + "/" + r.Name + "/{id}/save"

		p.mux.HandleFunc("GET "+newPath, p.auth.Require(func(w http.ResponseWriter, req *http.Request) {
			shell.SecurityHeaders(w)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			nav := p.activeNav(r.Name)
			tok := csrf.Issue(p.csrfKey, p.sessionValue(req), csrf.DefaultTTL)
			d := formPageData{
				Resource:  r,
				Values:    map[string]string{},
				CSRFToken: tok,
				BasePath:  p.basePath,
			}
			layoutComp := shell.Layout(p.title, nav, formPageContent(d))
			if err := layoutComp.Render(req.Context(), w); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}))

		p.mux.HandleFunc("GET "+editPath, p.auth.Require(func(w http.ResponseWriter, req *http.Request) {
			shell.SecurityHeaders(w)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			id := req.PathValue("id")
			t := tenant.From(req.Context())
			values, err := r.Writer.Load(req.Context(), t, id)
			if err != nil {
				http.Error(w, "load failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			nav := p.activeNav(r.Name)
			tok := csrf.Issue(p.csrfKey, p.sessionValue(req), csrf.DefaultTTL)
			d := formPageData{
				Resource:  r,
				ID:        id,
				Values:    values,
				CSRFToken: tok,
				BasePath:  p.basePath,
			}
			layoutComp := shell.Layout(p.title, nav, formPageContent(d))
			if err := layoutComp.Render(req.Context(), w); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}))

		p.mux.HandleFunc("POST "+savePath, p.auth.Require(func(w http.ResponseWriter, req *http.Request) {
			shell.SecurityHeaders(w)
			const maxFormBytes = 1 << 20 // 1 MB
			req.Body = http.MaxBytesReader(w, req.Body, maxFormBytes)
			if err := req.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}

			// CSRF check.
			token := req.FormValue(csrf.FormField)
			if err := csrf.Verify(p.csrfKey, p.sessionValue(req), token); err != nil {
				http.Error(w, "forbidden: "+err.Error(), http.StatusForbidden)
				return
			}

			id := req.PathValue("id")
			if id == "new" {
				id = ""
			}

			// Collect values.
			values := make(map[string]string, len(r.Writer.Form.Fields))
			for _, fld := range r.Writer.Form.Fields {
				if fld.Kind == FieldCheckbox {
					// Unchecked checkboxes are not submitted; normalise to "true"/"false".
					val := req.FormValue(fld.Key)
					if val == "true" || val == "on" || val == "1" {
						values[fld.Key] = "true"
					} else {
						values[fld.Key] = "false"
					}
				} else {
					values[fld.Key] = req.FormValue(fld.Key)
				}
			}

			// Server-side validation.
			errs := r.Writer.Form.validate(values)
			if errs.hasErrors() {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusUnprocessableEntity)
				nav := p.activeNav(r.Name)
				tok := csrf.Issue(p.csrfKey, p.sessionValue(req), csrf.DefaultTTL)
				d := formPageData{
					Resource:  r,
					ID:        id,
					Values:    values,
					Errors:    errs,
					CSRFToken: tok,
					BasePath:  p.basePath,
				}
				layoutComp := shell.Layout(p.title, nav, formPageContent(d))
				if err := layoutComp.Render(req.Context(), w); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}

			// Persist.
			t := tenant.From(req.Context())
			if err := r.Writer.Save(req.Context(), t, id, values); err != nil {
				http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// Redirect to list (PRG pattern).
			if render.IsHTMX(req) {
				w.Header().Set("HX-Redirect", p.basePath+"/"+r.Name)
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Redirect(w, req, p.basePath+"/"+r.Name, http.StatusSeeOther)
		}))
	}
}

// sessionValue extracts the session cookie value from the request.
// Used as the binding input for CSRF tokens.
func (p *Panel) sessionValue(r *http.Request) string {
	name := "panel_admin"
	if sc, ok := p.auth.(sessionCookier); ok {
		name = sc.SessionCookieName()
	}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// activeNav returns a copy of the nav with the given resource ID marked active.
func (p *Panel) activeNav(activeID string) []shell.NavItem {
	nav := make([]shell.NavItem, len(p.nav))
	copy(nav, p.nav)
	for i := range nav {
		nav[i].Active = nav[i].ID == activeID
	}
	return nav
}

// makeListHandler builds the handler func for a resource's list page.
func (p *Panel) makeListHandler(r Resource) func(http.ResponseWriter, *http.Request, []shell.NavItem, bool) {
	return func(w http.ResponseWriter, req *http.Request, nav []shell.NavItem, fragmentOnly bool) {
		ctx := req.Context()
		q := req.URL.Query()

		// Resolve sort state.
		sortState := r.Sort.Resolve(q.Get("sort"), q.Get("dir"))

		// Compose WHERE from filter params.
		// startArg starts at 1 since tenant scope will be appended AFTER filter args.
		whereConds, whereArgs := r.Filter.Where(q, 1)

		// Tenant scope — append after filter args so arg indices stay correct.
		t := tenant.From(ctx)
		var tenantCond string
		if r.Scope.Column != "" {
			tenantArg := len(whereArgs) + 1
			tc, ta := tenant.ScopeClause(r.Scope, t, tenantArg)
			if tc != "" {
				tenantCond = tc
				whereArgs = append(whereArgs, ta)
			}
		}

		// Combine WHERE conditions.
		finalConds := combineConds(whereConds, tenantCond)

		// Paging.
		page := max(1, parseIntParam(q.Get("page"), 1))
		pageSize := clampInt(parseIntParam(q.Get("per_page"), defaultPageSize), 1, maxPageSize)
		offset := (page - 1) * pageSize

		lq := ListQuery{
			Sort:       sortState,
			WhereConds: finalConds,
			WhereArgs:  whereArgs,
			Tenant:     t,
			Limit:      pageSize,
			Offset:     offset,
		}

		rows, total, err := r.Lister(ctx, lq)
		if err != nil {
			http.Error(w, "list failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		totalPages := max(1, (total+pageSize-1)/pageSize)

		data := listPageData{
			Resource:    r,
			Rows:        rows,
			SortState:   sortState,
			Page:        page,
			PageSize:    pageSize,
			Total:       total,
			TotalPages:  totalPages,
			BasePath:    p.basePath,
			QueryString: req.URL.RawQuery,
		}

		if fragmentOnly || render.IsHTMX(req) {
			c := listRowsFragment(data)
			if err := c.Render(ctx, w); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		content := listPageContent(data)
		layoutComp := shell.Layout(p.title, nav, content)
		if err := layoutComp.Render(ctx, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// --- helpers ---

func combineConds(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == "" && b == "":
		return ""
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " AND " + b
	}
}

func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return def
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
