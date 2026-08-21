// notice_test.go — falsification for the Notice block.
//
// Every assertion goes through NoticeView rather than calling noticeClass /
// noticeRole directly: a test that calls the mapper itself stays green when the
// template stops using it, which is how a helper ends up correct and unreached.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
)

func renderNotice(t *testing.T, n components.Notice) string {
	t.Helper()
	var buf bytes.Buffer
	if err := components.NoticeView(n).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// TestNoticeView_EmptyTextRendersNothing — callers compute these messages
// conditionally ("N clicks are missing from this total"). Without the guard,
// a zero-count caller leaves a coloured empty box on the page, and every caller
// needs its own `if` to avoid it — which is the guard, written N times.
//
// Falsification: drop the `if n.Text != ""` wrapper in notice.templ → RED.
func TestNoticeView_EmptyTextRendersNothing(t *testing.T) {
	for name, n := range map[string]components.Notice{
		"bare":           {},
		"title only":     {Title: "Heads up"},
		"link only":      {LinkHref: "/admin/clients", LinkText: "Clients"},
		"danger no text": {Level: components.NoticeDanger},
	} {
		if got := renderNotice(t, n); got != "" {
			t.Errorf("%s rendered %q, want nothing at all", name, got)
		}
	}
	if got := renderNotice(t, components.Notice{Text: "x"}); got == "" {
		t.Error("a notice WITH text rendered nothing — the guard is too wide")
	}
}

// TestNoticeView_LevelSelectsTheClass — the level is the whole severity signal.
// An unknown level must fall back to info, never to an unstyled block: an
// unstyled div on a money screen looks like plain prose, so a danger notice
// would silently stop looking like one.
//
// Falsification: in noticeClass, `return ""` in the default branch → RED.
func TestNoticeView_LevelSelectsTheClass(t *testing.T) {
	for _, tc := range []struct {
		level components.NoticeLevel
		want  string
	}{
		{components.NoticeInfo, "notice notice-info"},
		{components.NoticeWarn, "notice notice-warn"},
		{components.NoticeDanger, "notice notice-danger"},
		{components.NoticeMuted, "notice notice-muted"},
		{components.NoticeLevel(99), "notice notice-info"},
	} {
		body := renderNotice(t, components.Notice{Level: tc.level, Text: "msg"})
		if !strings.Contains(body, `class="`+tc.want+`"`) {
			t.Errorf("level %d rendered %q, want class %q", tc.level, body, tc.want)
		}
	}
}

// TestNoticeView_OnlyUrgentLevelsAnnounce — role="alert" interrupts a screen
// reader. A footnote saying "this is an estimate" must not; a blocked action
// must. Getting this backwards is invisible to a sighted reviewer.
//
// Falsification: in noticeRole, `return "alert"` unconditionally → RED.
func TestNoticeView_OnlyUrgentLevelsAnnounce(t *testing.T) {
	for _, tc := range []struct {
		level components.NoticeLevel
		want  string
	}{
		{components.NoticeDanger, `role="alert"`},
		{components.NoticeWarn, `role="alert"`},
		{components.NoticeInfo, `role="note"`},
		{components.NoticeMuted, `role="note"`},
	} {
		if body := renderNotice(t, components.Notice{Level: tc.level, Text: "msg"}); !strings.Contains(body, tc.want) {
			t.Errorf("level %d rendered %q, want %s", tc.level, body, tc.want)
		}
	}
}

// TestNoticeView_LinkNeedsBothHalves — a href with no text is an invisible
// link; text with no href is a word that looks clickable and is not. Both are
// worse than no link.
//
// Falsification: change the `&&` in the link condition to `||` → RED.
func TestNoticeView_LinkNeedsBothHalves(t *testing.T) {
	full := renderNotice(t, components.Notice{Text: "msg", LinkHref: "/admin/clients", LinkText: "Clients"})
	if !strings.Contains(full, `<a href="/admin/clients">Clients</a>`) {
		t.Errorf("a complete link did not render: %q", full)
	}
	for name, n := range map[string]components.Notice{
		"href only": {Text: "msg", LinkHref: "/admin/clients"},
		"text only": {Text: "msg", LinkText: "Clients"},
	} {
		if body := renderNotice(t, n); strings.Contains(body, "<a ") {
			t.Errorf("%s rendered an anchor: %q", name, body)
		}
	}
}

// TestNoticeView_EscapesEveryField — Title/Text come from the consumer and can
// carry a database value (a place name, an operator note). A notice is the last
// place anyone would look for an injection, which is why it is worth pinning.
func TestNoticeView_EscapesEveryField(t *testing.T) {
	body := renderNotice(t, components.Notice{
		Level:    components.NoticeDanger,
		Title:    `<script>a</script>`,
		Text:     `<img src=x onerror=1>`,
		LinkHref: "/admin/x",
		LinkText: `<b>go</b>`,
	})
	for _, bad := range []string{"<script>", "<img src=x", "<b>go</b>"} {
		if strings.Contains(body, bad) {
			t.Errorf("raw markup %q survived into the output: %q", bad, body)
		}
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("the title was not escaped into the output at all: %q", body)
	}
}

// TestNoticeView_RefusesAJavascriptURL — LinkHref is passed to templ as a plain
// string precisely so templ sanitises it. Wrapping it in templ.SafeURL would
// assert a safety this component cannot check, and a consumer building the href
// from data would then ship a live javascript: link.
//
// Falsification: wrap the href in templ.SafeURL(n.LinkHref) → RED.
func TestNoticeView_RefusesAJavascriptURL(t *testing.T) {
	body := renderNotice(t, components.Notice{
		Text: "msg", LinkHref: "javascript:alert(1)", LinkText: "click",
	})
	if strings.Contains(body, "javascript:") {
		t.Errorf("a javascript: href reached the output: %q", body)
	}
}
