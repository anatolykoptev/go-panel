package resource_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// ExampleNew constructs a minimal Panel and serves one request through it --
// the smallest working wiring: an Authenticator plus New produce a real
// http.Handler.
func ExampleNew() {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("example-hmac-signing-key-32bytes"),
		BasePath: "/admin",
	})
	p := resource.New(resource.Config{
		Title:    "Example Admin",
		BasePath: "/admin",
		Auth:     a,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	fmt.Println(w.Code)
	// Output: 200
}

// ExampleRegister declares a Resource and registers it, then serves its
// generated list page -- the core "declare a Resource, get a working admin
// page" pattern this package exists to provide.
func ExampleRegister() {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("example-hmac-signing-key-32bytes"),
		BasePath: "/admin",
	})
	p := resource.New(resource.Config{BasePath: "/admin", Auth: a})

	resource.Register(p, resource.Resource{
		Name:  "items",
		Title: "Items",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "name", Label: "Name", Sortable: true, SQLExpr: "name"}},
			DefaultKey: "name",
			DefaultDir: admintable.Asc,
		},
		Lister: func(_ context.Context, _ resource.ListQuery) ([]resource.Row, int, error) {
			return []resource.Row{{ID: "1", Cells: []resource.Cell{{Value: "Alpha"}}}}, 1, nil
		},
	})

	nav := p.NavItems()
	fmt.Println(len(nav), nav[0].URL)
	// Output: 1 /admin/items
}

// ExamplePanel_MountPage mounts a custom admin page -- for a handler that
// isn't a Resource-generated list, e.g. a dashboard -- guarded by the same
// auth every Resource route uses.
func ExamplePanel_MountPage() {
	a := auth.NewHMACAuth(auth.HMACConfig{
		Username: "admin",
		Password: "secret",
		HMACKey:  []byte("example-hmac-signing-key-32bytes"),
		BasePath: "/admin",
	})
	p := resource.New(resource.Config{BasePath: "/admin", Auth: a})

	p.MountPage(resource.PageSpec{
		Path: "dashboard",
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("dashboard"))
		},
	})

	// Unauthenticated request: the mounted handler is guarded the same way
	// a Resource route is, so it redirects to the login page rather than
	// running the handler.
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/", nil)
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	fmt.Println(w.Code, w.Header().Get("Location"))
	// Output: 303 /admin/login
}
