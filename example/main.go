// Example is a minimal runnable demo of go-panel.
// It registers one in-memory Resource (no DB required) and serves the admin UI.
//
//	go run ./example
//	open http://localhost:8080/admin/
//	username: admin  password: demo
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// staticItems is the in-memory dataset for the demo resource.
const (
	placeTypeCafe       = "cafe"
	placeTypeRestaurant = "restaurant"
	placeTypeBar        = "bar"
	statusPublished     = "published"
	statusDraft         = "draft"
)

var staticItems = []resource.Row{
	{ID: "1", Cells: []resource.Cell{{Value: "Кофейня Симпл"}, {Value: placeTypeCafe}, {Value: statusPublished}}, Href: "/admin/places/1"},
	{ID: "2", Cells: []resource.Cell{{Value: "Хинкальная Дарьял"}, {Value: placeTypeRestaurant}, {Value: statusPublished}}, Href: "/admin/places/2"},
	{ID: "3", Cells: []resource.Cell{{Value: "Бар Голубка"}, {Value: placeTypeBar}, {Value: statusDraft}}},
	{ID: "4", Cells: []resource.Cell{{Value: "Пиццерия Дио Мио"}, {Value: placeTypeRestaurant}, {Value: statusPublished}}},
	{ID: "5", Cells: []resource.Cell{{Value: "Кафе-бар Street Food"}, {Value: placeTypeCafe}, {Value: statusPublished}}},
}

func placesLister(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
	// In-memory filter by status (demo only — real lister uses q.WhereConds with pgx).
	var filtered []resource.Row
	// Demo: serve all rows regardless of filter (real lister passes
	// q.WhereConds+q.WhereArgs to a pgx query). The filter bar still renders;
	// it just has no effect on the static in-memory slice.
	filtered = append(filtered, staticItems...)
	// Apply paging.
	start := q.Offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + q.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], len(filtered), nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	hmacKey := os.Getenv("ADMIN_HMAC_KEY")
	if hmacKey == "" {
		hmacKey = "dev-only-key-change-in-production!"
	}
	username := "admin"
	password := "demo"

	a := auth.NewHMACAuth(auth.HMACConfig{
		Username:   username,
		Password:   password,
		HMACKey:    []byte(hmacKey),
		BasePath:   "/admin",
		SessionTTL: 12 * time.Hour,
		Secure:     false, // dev only — set true in prod behind TLS
	})

	p := resource.New(resource.Config{
		Title:    "go-panel demo",
		BasePath: "/admin",
		Auth:     a,
		Resolver: tenant.PathResolver{Segment: 2},
	})

	placesResource := resource.Resource{
		Name:  "places",
		Title: "Places",
		Icon:  "📍",
		Group: "Content",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Label: "Name", Sortable: true, SQLExpr: "p.name"},
				{Key: "category", Label: "Category", Sortable: true, SQLExpr: "p.category_slug"},
				{Key: "status", Label: "Status", Sortable: false, SQLExpr: "p.status"},
			},
			DefaultKey: "name",
			DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "status", SQLExpr: "p.status", Match: admintable.Eq, Allowed: []string{"published", "draft"}},
			{Key: "category", SQLExpr: "p.category_slug", Match: admintable.Eq, Allowed: []string{"cafe", "restaurant", "bar"}},
			{Key: "q", SQLExprs: []string{"p.name"}, Match: admintable.ILike},
		}},
		Scope:  tenant.Scope{Column: "p.city_slug"},
		Lister: placesLister,
	}

	resource.Register(p, placesResource)

	mux := http.NewServeMux()
	mux.Handle("/admin/", p.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})

	addr := ":8080"
	logger.Info("go-panel example listening", "addr", addr, "username", username, "password", password)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
