package resource

import (
	"net/http"
	"strings"
)

// PageSpec declares a custom admin page to mount into the Panel's own mux via
// MountPage, so Handler() serves it alongside the built-in resource routes —
// no external ServeMux wrapper required.
type PageSpec struct {
	// Path is the URL suffix under BasePath, leading/trailing slashes trimmed.
	// "" or "/" mounts the INDEX page (GET {basePath}/{$}), replacing the
	// default redirect-to-first-resource. A non-empty value, e.g.
	// "billing_preview", mounts a sub-page at GET {basePath}/billing_preview/{$}.
	Path string

	// Handler serves the page — typically by calling p.RenderPage. It is
	// wrapped with the Panel's guard (the same auth/role gate resources use)
	// before mounting. Required: MountPage panics if nil.
	Handler http.HandlerFunc

	// RequiredRole gates the page via the same mechanism as Resource.RequiredRole:
	// "" allows any authenticated session; a non-empty role requires the
	// configured authenticator to implement RoleAuthenticator, validated
	// EAGERLY at MountPage — panics otherwise (fail-closed).
	RequiredRole string

	// Aliases are extra exact-match GET suffixes serving the same guarded
	// handler (e.g. []string{"overview"} mounts GET {basePath}/overview).
	// Aliases are exact matches — no {$} pattern, no trailing-slash redirect.
	Aliases []string
}

// MountPage registers a custom admin page into the Panel's own mux, wrapped
// with the same auth/role guard Register uses for resources. It must be
// called before the first Handler() call, which finalizes the mux; calling
// MountPage afterward panics.
//
// spec.Path == "" (or "/") mounts the page as the panel's index, replacing
// the default redirect-to-first-resource. Only one page may claim the index —
// a second Path:"" MountPage call panics.
//
// MountPage is routing only: it does not touch the sidebar nav. Consumers
// that want a nav entry for the page still call AddNav themselves (nav order
// matters to them and MountPage must not reorder it).
func (p *Panel) MountPage(spec PageSpec) {
	if p.finalized {
		panic("resource: MountPage called after Handler() — pages must be mounted before the mux is finalized")
	}
	if spec.Handler == nil {
		panic("resource: MountPage requires a non-nil Handler")
	}
	// guard() eagerly validates RequiredRole: it panics here (mount time) if
	// the authenticator can't back a non-empty role, rather than failing open
	// at request time.
	guarded := p.guard(spec.RequiredRole, spec.Handler)

	suffix := strings.Trim(spec.Path, "/")
	if suffix == "" {
		if p.indexOverride != nil {
			panic(`resource: MountPage called with Path:"" twice — only one page may be the index`)
		}
		p.indexOverride = guarded
	} else {
		p.mux.HandleFunc("GET "+p.basePath+"/"+suffix+"/{$}", guarded)
	}

	for _, alias := range spec.Aliases {
		a := strings.Trim(alias, "/")
		if a == "" {
			panic("resource: MountPage alias must not be empty")
		}
		p.mux.HandleFunc("GET "+p.basePath+"/"+a, guarded)
	}
}

// finalize mounts the index route exactly once: the MountPage-supplied
// override if MountPage(PageSpec{Path: ""}) was called, otherwise the
// default handleIndex. It runs on the first Handler() call so the mux is
// immutable after that point — no per-request mutable-field read (which
// would be a data race) — and finalized makes further MountPage calls fail
// closed instead of silently no-op'ing.
func (p *Panel) finalize() {
	p.finalizeOnce.Do(func() {
		h := p.indexOverride
		if h == nil {
			h = p.guard("", p.handleIndex)
		}
		p.mux.HandleFunc("GET "+p.basePath+"/{$}", h)
		p.finalized = true
	})
}
