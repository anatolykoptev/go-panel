package resource

import (
	"strings"
	"testing"
)

func TestCrossLinkCell_RendersAnchor(t *testing.T) {
	got := CrossLinkCell("/admin", "users", "42", "Alice")
	want := `<a href="/admin/users/42">Alice</a>`
	if got != want {
		t.Fatalf("CrossLinkCell = %q, want %q", got, want)
	}
}

func TestCrossLinkCell_XSSLabelEscaped(t *testing.T) {
	got := CrossLinkCell("/admin", "users", "42", "<script>alert(1)</script>")
	if strings.Contains(got, "<script>") {
		t.Fatalf("output contains raw <script> tag (stored XSS): %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("label was not HTML-escaped as expected: %q", got)
	}
}

func TestCrossLinkCell_XSSIdPathEscaped(t *testing.T) {
	got := CrossLinkCell("/admin", "users", "../evil", "label")
	// url.PathEscape turns "../evil" into "..%2Fevil", neutralizing path traversal.
	if strings.Contains(got, "/../evil") {
		t.Fatalf("path traversal payload not neutralized in href: %q", got)
	}
	if !strings.Contains(got, "..%2Fevil") {
		t.Fatalf("expected url.PathEscape'd id segment ..%%2Fevil, got: %q", got)
	}
}

func TestCrossLinkCell_URLEscapingNotHTMLEscaping(t *testing.T) {
	// Regression test for the go-grad bug: html.EscapeString was applied to the
	// id inside the href, turning "&" into "&amp;" — wrong for a URL path
	// context. url.PathEscape leaves "&" as-is in a path segment, which is
	// correct. The output must NOT contain "&amp;" for the id.
	got := CrossLinkCell("/admin", "users", "a&b", "label")
	if strings.Contains(got, "&amp;") {
		t.Fatalf("id '&' was wrongly HTML-escaped to &amp; in href (the bug): %q", got)
	}
	if !strings.Contains(got, "/a&b") {
		t.Fatalf("expected unescaped '&b' id segment in href, got: %q", got)
	}
}

func TestCrossLinkCellInt_ConvertsInt64(t *testing.T) {
	got := CrossLinkCellInt("/admin", "users", 99, "Ninety-Nine")
	want := `<a href="/admin/users/99">Ninety-Nine</a>`
	if got != want {
		t.Fatalf("CrossLinkCellInt = %q, want %q", got, want)
	}
}
