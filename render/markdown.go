package render

// Markdown rendering via goldmark, ported from go-nerv/internal/admin/markdown.go,
// plus link/image hardening (safeLinks) and the Prose wrapper.

import (
	"bytes"
	"html/template"
	"net/url"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// headingDemoter is a goldmark AST transformer that demotes all heading levels
// by exactly +1 (h1→h2, h2→h3, ..., h6→h6 clamped). This prevents content-
// authored <h1> from reversing WCAG 1.3.1 heading order relative to the page's
// <h2> title header in admin views.
type headingDemoter struct{}

func (headingDemoter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			const maxHeadingLevel = 6
			if h.Level < maxHeadingLevel {
				h.Level++
			}
			// h6 clamped: stays at 6.
		}
		return ast.WalkContinue, nil
	})
}

// linkRel is stamped on every rendered link. The content is operator-visible
// model output over web content, so a link must not hand the target a window
// reference (noopener), a referrer (noreferrer) or search credit (nofollow).
const linkRel = "noopener noreferrer nofollow"

// safeLinks is a goldmark AST transformer that hardens links and images:
//
//   - every link and autolink gets rel="noopener noreferrer nofollow";
//   - an image whose destination names a host (http(s), protocol-relative, any
//     other scheme://) is rendered as a link instead of an <img>, because a
//     remote <img> is a beacon that fires when the page is opened. Local paths
//     and data: images stay images. Page CSP (img-src 'self' data:) already
//     blocks the fetch on panel pages; this makes the output safe on its own.
type safeLinks struct{}

func (safeLinks) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	var images []*ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Link:
			v.SetAttributeString("rel", []byte(linkRel))
		case *ast.AutoLink:
			v.SetAttributeString("rel", []byte(linkRel))
		case *ast.Image:
			if isRemote(string(v.Destination)) {
				images = append(images, v)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, img := range images {
		parent := img.Parent()
		if parent == nil {
			continue
		}
		if insideLink(img) {
			// A link cannot nest another link: keep only the alt text.
			for c := img.FirstChild(); c != nil; {
				next := c.NextSibling()
				parent.InsertBefore(parent, img, c)
				c = next
			}
			parent.RemoveChild(parent, img)
			continue
		}
		link := ast.NewLink()
		link.Destination = img.Destination
		link.Title = img.Title
		link.SetAttributeString("rel", []byte(linkRel))
		for c := img.FirstChild(); c != nil; {
			next := c.NextSibling()
			link.AppendChild(link, c)
			c = next
		}
		if link.FirstChild() == nil {
			// Empty alt text: an empty anchor is invisible and unlabelled.
			link.AppendChild(link, ast.NewString(img.Destination))
		}
		parent.ReplaceChild(parent, img, link)
	}
}

// isRemote reports whether an image destination names a host. An unparsable
// destination is treated as remote: the safe side of the choice.
func isRemote(dest string) bool {
	u, err := url.Parse(dest)
	if err != nil {
		return true
	}
	return u.Host != ""
}

func insideLink(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.Link); ok {
			return true
		}
	}
	return false
}

// md is a process-global goldmark instance with GFM extensions enabled
// (tables, strikethrough, task lists, autolinks), safe HTML rendering
// (no raw <script>), and heading-level demotion (+1 per level).
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(
			util.Prioritized(headingDemoter{}, 100),
			util.Prioritized(safeLinks{}, 200),
		),
	),
	// WithUnsafe() is intentionally omitted — default is safe rendering.
)

// Prose renders markdown inside <div class="prose">, the wrapper that carries
// the panel's reading styles (measure, lists, tables, code). Use it for
// report-style content; Markdown stays the bare fragment for callers that
// style their own container.
func Prose(src string) template.HTML {
	body := Markdown(src)
	if body == "" {
		return ""
	}
	return template.HTML(`<div class="prose">` + string(body) + `</div>`) //nolint:gosec // body is goldmark safe output
}

// Markdown converts markdown source to safe HTML via goldmark.
// On parse error, returns an HTML-escaped fallback paragraph — never panics,
// never returns empty string for non-empty input.
func Markdown(src string) template.HTML {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return template.HTML("<p class=\"md-error\">render failed: " + template.HTMLEscapeString(err.Error()) + "</p>") //nolint:gosec // operator-only admin surface
	}
	return template.HTML(buf.String()) //nolint:gosec // goldmark safe-by-default strips raw HTML
}
