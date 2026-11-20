package resource

import (
	"bytes"
	"context"
	"html"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/anatolykoptev/go-panel/components"
)

// BadgeCell renders a status chip as the HTML of a list cell
// (Cell{Value: BadgeCell(tone, "pending"), HTML: true}). The text is escaped
// and the tone is the closed [components.Tone] set, so neither a database value
// nor a caller can put markup or a class name into the page.
func BadgeCell(tone components.Tone, text string) string {
	return renderCell("BadgeCell", components.Badge(tone, text), text)
}

// DiffCell renders an old -> new change as the HTML of a list cell
// (Cell{Value: DiffCell("1", "2"), HTML: true}): the old value struck through,
// the new one bold. Both values are escaped.
func DiffCell(from, to string) string {
	return renderCell("DiffCell", components.Diff(from, to), to)
}

// renderCell renders a component to a string for Cell{HTML: true}. Rendering
// into a bytes.Buffer cannot fail; if that ever changes, log and fall back to
// the escaped text rather than an empty cell.
func renderCell(name string, c templ.Component, fallback string) string {
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		slog.Error("resource: "+name+" render failed", "err", err)
		return html.EscapeString(fallback)
	}
	return b.String()
}

// listHeader resolves Resource.Header for one request: nil when the resource
// declares none, or when it returns no component.
func (r Resource) listHeader(ctx context.Context, req *http.Request) templ.Component {
	if r.Header == nil {
		return nil
	}
	return r.Header(ctx, req)
}
