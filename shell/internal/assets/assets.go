// Package assets embeds the shell static assets (htmx, pm7, admin.js, skin fonts).
package assets

import "embed"

//go:embed static/htmx.min.js static/pm7.css static/pm7.js static/admin.js static/fonts/*.woff2 static/fonts/LICENSE-*.txt
var StaticFS embed.FS
