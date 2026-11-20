package resource_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/i18n"
	"github.com/anatolykoptev/go-panel/resource"
)

func headResource() resource.Resource {
	return detailTableResource("things", []resource.DetailSection{{Title: "S", Items: []resource.DetailItem{{Label: "L", Value: "V"}}}})
}

// RED-on-revert: drop the d.Title assignment in the detail handler
// (resource/resource.go, `d.Title = r.DetailTitle(...)`) -> the named-record
// case renders the unnamed heading.
func TestDetailHeader_NamedAndUnnamed(t *testing.T) {
	r := headResource()
	r.DetailTitle = func(context.Context, *http.Request, string) string { return "alpha_share" }
	body := renderDetail(t, r, "7")
	for _, want := range []string{`<h2>alpha_share</h2>`, `<p class="detail-head-sub">`, ` · 7</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("named header missing %q", want)
		}
	}

	body = renderDetail(t, headResource(), "7")
	if !strings.Contains(body, `<p class="detail-head-sub">7</p>`) || strings.Contains(body, "alpha_share") {
		t.Errorf("unnamed header must show the resource title over the id")
	}
}

// The actions slot renders the consumer's component, only when declared.
//
// RED-on-revert: drop `d.Actions = r.DetailActions(...)` in the handler.
func TestDetailHeader_ActionsSlot(t *testing.T) {
	r := headResource()
	r.DetailActions = func(_ context.Context, _ *http.Request, id string) templ.Component {
		return templ.Raw(`<a class="btn" href="/x/` + id + `">Revert</a>`)
	}
	body := renderDetail(t, r, "7")
	if !strings.Contains(body, `<div class="detail-actions">`) || !strings.Contains(body, `<a class="btn" href="/x/7">Revert</a>`) {
		t.Errorf("actions slot missing")
	}
	if strings.Contains(renderDetail(t, headResource(), "7"), `class="detail-actions"`) {
		t.Errorf("no DetailActions must render no slot")
	}
}

// Hand-computed oracle for the relative-time buckets (English preset).
func TestTimeCellAt_Buckets(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-10 * time.Second, "just now"},
		{-44 * time.Second, "just now"},
		{-45 * time.Second, "1 min ago"},
		{-59 * time.Minute, "59 min ago"},
		{-60 * time.Minute, "1 h ago"},
		{-23*time.Hour - 59*time.Minute, "23 h ago"},
		{-24 * time.Hour, "1 d ago"},
		{-49 * time.Hour, "2 d ago"},
		{3 * time.Hour, "in 3 h"},
		{5 * time.Minute, "in 5 min"},
	}
	for _, tc := range cases {
		c := resource.TimeCellAt(context.Background(), now.Add(tc.d), now)
		if !c.HTML || !strings.Contains(c.Value, ">"+tc.want+"</time>") {
			t.Errorf("%v: got %q, want relative %q", tc.d, c.Value, tc.want)
		}
	}
	c := resource.TimeCellAt(context.Background(), now.Add(-2*time.Hour), now)
	for _, want := range []string{`datetime="2026-11-20T10:00:00Z"`, `title="2026-11-20 10:00:00 UTC"`, `<span class="time-abs">2026-11-20 10:00</span>`} {
		if !strings.Contains(c.Value, want) {
			t.Errorf("missing %s in %s", want, c.Value)
		}
	}
	if z := resource.TimeCell(context.Background(), time.Time{}); z.Value != "" || z.HTML {
		t.Errorf("zero time must be an empty cell, got %+v", z)
	}
	if it := resource.TimeItemAt(context.Background(), "L", time.Time{}, now); it.Value != "" || it.HTML {
		t.Errorf("zero time item must be empty")
	}
}

// A consumer's RelativeTime is free text: it must be escaped even though the
// cell is raw HTML.
//
// RED-on-revert: drop html.EscapeString in timeHTML (resource/timecell.go).
func TestTimeCell_EscapesConsumerWording(t *testing.T) {
	ctx := i18n.WithMerged(context.Background(), i18n.Strings{RelativeTime: func(time.Duration) string { return `<script>x</script>` }})
	now := time.Now()
	c := resource.TimeCellAt(ctx, now, now)
	if strings.Contains(c.Value, "<script>") || !strings.Contains(c.Value, "&lt;script&gt;") {
		t.Errorf("consumer wording not escaped: %s", c.Value)
	}
}
