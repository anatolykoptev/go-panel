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
