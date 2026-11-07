package main

import (
	"net/http"
	"sync"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/resource"
)

// collectSource is one switchable source of the fake Collection page.
type collectSource struct {
	ID, Label, Status string
	On                bool
}

var (
	collectMu      sync.Mutex
	collectSources = []collectSource{
		{ID: "news", Label: "News feed", Status: "last run 4 min ago", On: true},
		{ID: "filings", Label: "SEC filings", Status: "last run 1 h ago", On: true},
		{ID: "social", Label: "Social posts", Status: "paused", On: false},
	}
)

func collectRows(token string, sources []collectSource) []components.SettingsRow {
	rows := make([]components.SettingsRow, 0, len(sources))
	for _, s := range sources {
		rows = append(rows, components.SettingsRow{
			Label: s.Label, Hint: s.Status,
			Control: components.SwitchView(components.Switch{
				Action: "/admin/settings/collection/toggle", Token: token, On: s.On,
				Label: s.Label, OnText: "On", OffText: "Off",
				Fields: []components.SwitchField{{Name: "source", Value: s.ID}},
			}),
		})
	}
	return rows
}

// mountSettingsDemo mounts the settings area with the fake workspace page.
// role gates the page and its switch (empty on the single-user variant, whose
// authenticator has no roles).
func mountSettingsDemo(p *resource.Panel, cfg resource.SettingsConfig, role string) {
	cfg.Pages = append(cfg.Pages, resource.SettingsPage{
		ID: "collection", Title: "Collection", RequiredRole: role,
		Subtitle: "What this panel collects.",
		BodyFunc: func(r *http.Request) templ.Component {
			collectMu.Lock()
			defer collectMu.Unlock()
			return collectionBody(p.CSRFToken(r), append([]collectSource(nil), collectSources...))
		},
	})
	p.MountSettings(cfg)
	// Inherits the page's role: a manager's POST is refused like the page is.
	p.MountSettingsAction("collection", resource.ActionSpec{
		Path: "toggle",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			collectMu.Lock()
			for i := range collectSources {
				if collectSources[i].ID == r.PostForm.Get("source") {
					collectSources[i].On = r.PostForm.Get("enabled") == "1"
				}
			}
			collectMu.Unlock()
			http.Redirect(w, r, "/admin/settings/collection/", http.StatusSeeOther)
		},
	})
}
