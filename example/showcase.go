package main

import (
	"net/http"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

const (
	showcaseID   = "showcase"
	showcasePath = "/admin/showcase/"
	labelWorking = "Working"
)

// mountShowcase mounts GET /admin/showcase/ (every redesign component) and two
// CSRF-verified POST actions the page's controls post to, so the tokens on the
// page are the real ones the panel issued for this session.
func mountShowcase(p *resource.Panel) {
	p.MountPage(resource.PageSpec{
		Path: showcaseID,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			d := showcaseData{Token: p.CSRFToken(r)}
			if err := p.RenderPage(w, r, "Showcase", showcaseID, Showcase(d)); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		},
	})
	p.AddNav(shell.NavItem{Group: "Demo", ID: "group:Demo"})
	p.AddNav(shell.NavItem{ID: showcaseID, Label: "Showcase", Icon: "◧", URL: showcasePath})

	// "Take" returns the re-rendered queue row for htmx; without JS it falls
	// back to a 303 to the page.
	p.MountAction(resource.ActionSpec{
		Path: "showcase/take",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("HX-Request") != "true" {
				http.Redirect(w, r, showcasePath, http.StatusSeeOther)
				return
			}
			row := components.QueueRow{
				ID: "q-1", Badge: labelWorking, BadgeTone: components.ToneBlue, Meta: "just now",
				Title: "Anna Belova", Subtitle: "Coffee shop", Secondary: "Taken by you",
				Actions: []components.RowAction{{Label: "Open", Variant: components.VariantGhost, Href: showcasePath}},
			}
			if err := components.QueueRowView(row).Render(r.Context(), w); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		},
	})
	p.MountAction(resource.ActionSpec{
		Path: "showcase/status/{slug}",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, showcasePath, http.StatusSeeOther)
		},
	})
}
