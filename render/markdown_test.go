package render_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/render"
)

func TestMarkdown_Empty(t *testing.T) {
	out := render.Markdown("")
	if out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestMarkdown_BasicParagraph(t *testing.T) {
	out := render.Markdown("hello world")
	if !strings.Contains(string(out), "<p>hello world</p>") {
		t.Errorf("expected <p>hello world</p>, got %q", out)
	}
}

func TestMarkdown_HeadingDemotion(t *testing.T) {
	out := render.Markdown("# Title")
	// h1 should be demoted to h2
	if strings.Contains(string(out), "<h1") {
		t.Errorf("expected h1 to be demoted, got %q", out)
	}
	if !strings.Contains(string(out), "<h2") {
		t.Errorf("expected h2 after demotion, got %q", out)
	}
}

func TestMarkdown_ScriptTagStripped(t *testing.T) {
	out := render.Markdown("<script>alert(1)</script> text")
	if strings.Contains(string(out), "<script>") {
		t.Errorf("script tag should be stripped, got %q", out)
	}
}

func TestMarkdown_GFMTable(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |"
	out := render.Markdown(src)
	if !strings.Contains(string(out), "<table>") {
		t.Errorf("expected GFM table, got %q", out)
	}
}

func TestMarkdown_RemoteImageBecomesLink(t *testing.T) {
	cases := map[string]string{
		"https":             "![chart](https://evil.example/x.png?d=secret)",
		"http":              "![chart](http://evil.example/x.png)",
		"protocol-relative": "![chart](//evil.example/x.png)",
		"other scheme":      "![chart](ftp://evil.example/x.png)",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out := string(render.Markdown(src))
			if strings.Contains(out, "<img") {
				t.Fatalf("remote image must not render as <img>, got %q", out)
			}
			if !strings.Contains(out, "<a href=\"") || !strings.Contains(out, ">chart</a>") {
				t.Fatalf("remote image must render as a link labelled by its alt text, got %q", out)
			}
		})
	}
}

func TestMarkdown_RemoteImageEmptyAltKeepsLabel(t *testing.T) {
	out := string(render.Markdown("![](https://evil.example/x.png)"))
	if strings.Contains(out, "<img") || !strings.Contains(out, ">https://evil.example/x.png</a>") {
		t.Fatalf("empty alt must fall back to the URL as link text, got %q", out)
	}
}

func TestMarkdown_LocalAndDataImagesStay(t *testing.T) {
	for _, src := range []string{"![a](/static/x.png)", "![a](x.png)", "![a](data:image/png;base64,AAAA)"} {
		if out := string(render.Markdown(src)); !strings.Contains(out, "<img") {
			t.Errorf("%q: local image must stay an <img>, got %q", src, out)
		}
	}
}

func TestMarkdown_ImageInsideLinkDoesNotNestAnchors(t *testing.T) {
	out := string(render.Markdown("[![badge](https://evil.example/b.png)](https://example.com/page)"))
	if strings.Contains(out, "<img") || strings.Count(out, "<a ") != 1 {
		t.Fatalf("want one anchor and no <img>, got %q", out)
	}
	if !strings.Contains(out, "badge") {
		t.Fatalf("alt text must survive, got %q", out)
	}
}

func TestMarkdown_LinksCarryRel(t *testing.T) {
	const rel = `rel="noopener noreferrer nofollow"`
	for name, src := range map[string]string{
		"inline":   "[a](https://example.com)",
		"autolink": "see https://example.com/page now",
		"angle":    "<https://example.com/page>",
		"image":    "![a](https://example.com/x.png)",
	} {
		t.Run(name, func(t *testing.T) {
			if out := string(render.Markdown(src)); !strings.Contains(out, rel) {
				t.Fatalf("missing %s in %q", rel, out)
			}
		})
	}
}

func TestMarkdown_JavascriptLinkNeutralised(t *testing.T) {
	out := string(render.Markdown("[x](javascript:alert(1)) and [y](JaVaScRiPt:alert(2))"))
	if strings.Contains(strings.ToLower(out), "javascript:") {
		t.Fatalf("javascript: URL must not reach href, got %q", out)
	}
}

func TestMarkdown_ImgOnerrorStripped(t *testing.T) {
	out := string(render.Markdown(`<img src=x onerror=alert(1)> and ![a](x.png "t" onerror=alert(1))`))
	if strings.Contains(out, "onerror=") && strings.Contains(out, "<img src=x") {
		t.Fatalf("raw HTML img with handler must be dropped, got %q", out)
	}
	if strings.Contains(out, "<img src=x") {
		t.Fatalf("raw HTML must not pass through, got %q", out)
	}
}

func TestProse_WrapsAndKeepsEmptyEmpty(t *testing.T) {
	if got := render.Prose(""); got != "" {
		t.Fatalf("empty source must stay empty, got %q", got)
	}
	out := string(render.Prose("hello"))
	if !strings.HasPrefix(out, `<div class="prose">`) || !strings.HasSuffix(out, "</div>") {
		t.Fatalf("want a .prose wrapper, got %q", out)
	}
}
