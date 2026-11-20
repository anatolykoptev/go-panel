package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/components"
)

func renderComp(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// A changed row strikes the old value and bolds the new; an unchanged row shows
// the value once. RED-on-revert: make diffChanged return true.
func TestDiff_ChangedAndUnchangedRows(t *testing.T) {
	out := renderComp(t, components.DiffView(components.Diff{Rows: []components.DiffRow{
		{Label: "alpha_share", From: "20 %", To: "25 %"},
		{Label: "cap", From: "18 %", To: "18 %"},
	}}))
	if !strings.Contains(out, `<del class="diff-from">20 %</del>`) || !strings.Contains(out, `<ins class="diff-to">25 %</ins>`) {
		t.Errorf("changed row not rendered as del/ins: %s", out)
	}
	if strings.Count(out, "<del") != 1 || !strings.Contains(out, `<span class="diff-same">18 %</span>`) {
		t.Errorf("unchanged row must render once, no del: %s", out)
	}
}

func TestDiff_EmptyRendersNothing(t *testing.T) {
	if out := renderComp(t, components.DiffView(components.Diff{})); out != "" {
		t.Errorf("empty diff rendered %q", out)
	}
}

// Stored values are text, never markup. RED-on-revert: render a field via templ.Raw.
func TestDiff_Escapes(t *testing.T) {
	out := renderComp(t, components.DiffView(components.Diff{Rows: []components.DiffRow{
		{Label: "<b>l</b>", From: "<i>a</i>", To: `"><script>x</script>`},
	}}))
	for _, bad := range []string{"<b>l", "<i>a", "<script>"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q leaked unescaped: %s", bad, out)
		}
	}
}

func TestConfirm_FormCarriesTokenAndFields(t *testing.T) {
	out := renderComp(t, components.ConfirmView(components.Confirm{
		Title: "Revert x", Message: "Sends a card.",
		Post: components.PostAction{URL: "/admin/settings/revert", Token: "tok123",
			Fields: []components.HiddenField{{Name: "key", Value: "alpha"}}},
		ConfirmLabel: "Send", CancelLabel: "Keep", CancelHref: "/admin/history/", Danger: true,
	}))
	for _, want := range []string{
		`<form method="POST" action="/admin/settings/revert" class="confirm-actions">`,
		`name="_csrf" value="tok123"`, `name="key" value="alpha"`,
		`class="btn btn-danger">Send</button>`, `<a class="btn btn-ghost" href="/admin/history/">Keep</a>`,
		`<h2>Revert x</h2>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if !strings.Contains(renderComp(t, components.ConfirmView(components.Confirm{Title: "t"})), `btn btn-primary`) {
		t.Errorf("default confirm button must be primary")
	}
}
