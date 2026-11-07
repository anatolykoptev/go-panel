package resource

import (
	"bytes"
	"context"
	"html"
	"log/slog"

	"github.com/anatolykoptev/go-panel/components"
)

// UnitStripCell renders a components.UnitRow as the HTML of a list cell
// (Cell{Value: UnitStripCell(row), HTML: true}), the way FilterLinkCell does
// for a link. Notes and titles are escaped and hrefs that are not same-origin
// paths drop the link; both are enforced by the component, not here.
func UnitStripCell(r components.UnitRow) string {
	var b bytes.Buffer
	if err := components.UnitStrip(r).Render(context.Background(), &b); err != nil {
		// Rendering into a bytes.Buffer cannot fail; log rather than swallow if
		// that ever changes, and fall back to the escaped row label as text.
		slog.Error("resource: UnitStripCell render failed", "err", err)
		return html.EscapeString(r.Label)
	}
	return b.String()
}
