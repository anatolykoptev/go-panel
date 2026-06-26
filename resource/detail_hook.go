package resource

import (
	"context"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// ErrDetailNotFound is returned by a Detail hook to signal a 404.
var ErrDetailNotFound = errors.New("resource: detail not found")

// withBackLink composes an html/template-escaped back-link above the hook content.
// listHref and resourceTitle are escaped via html/template before emission.
// content is rendered after the back-link.
func withBackLink(listHref string, resourceTitle string, content templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		escaped := template.HTMLEscapeString(listHref)
		labelEscaped := template.HTMLEscapeString(resourceTitle)
		_, err := io.WriteString(w, `<a href="`+escaped+`" class="back-link">← `+labelEscaped+`</a>`)
		if err != nil {
			return err
		}
		return content.Render(ctx, w)
	})
}

// mountDetailHookRoute mounts GET {basePath}/{name}/{id} for a Detail-hook-enabled resource.
// Called only when r.Detail != nil. id=="new" is rejected with 404.
func mountDetailHookRoute(p *Panel, r Resource) {
	detailPath := p.basePath + "/" + r.Name + "/{id}"
	p.mux.HandleFunc("GET "+detailPath, p.auth.Require(detailHookHandler(p, r)))
}

// detailHookHandler returns the handler for GET {basePath}/{name}/{id} when Detail is set.
// id=="new" → 404. ErrDetailNotFound from the hook → 404. Other errors → 500.
// Renders via p.RenderPage so the panel shell + nav active state are applied.
func detailHookHandler(p *Panel, r Resource) http.HandlerFunc {
	listHref := p.basePath + "/" + r.Name
	return func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("id")
		if id == idNew {
			http.NotFound(w, req)
			return
		}
		title, content, err := r.Detail(req.Context(), req, id)
		if err != nil {
			if errors.Is(err, ErrDetailNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			slog.Error("resource: detail hook failed", "resource", r.Name, "id", id, "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		composed := withBackLink(listHref, r.Title, content)
		if err := p.RenderPage(w, req, title, r.Name, composed); err != nil {
			slog.Error("resource: render detail hook page", "resource", r.Name, "id", id, "err", err)
		}
	}
}
