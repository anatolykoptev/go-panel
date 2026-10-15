package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/csrf"
)

const xss = `<script>alert(1)</script>`

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func mustNotContainRaw(t *testing.T, html string) {
	t.Helper()
	if strings.Contains(html, "<script>") {
		t.Errorf("unescaped <script> in output:\n%s", html)
	}
}

// TestCSRFField_MatchesVerifier pins the hidden-input name the components emit
// to the name the panel's verifier reads. components may not import csrf, so
// nothing but this test stops the two drifting (the delete-button 403 of
// 2026-08-20 was exactly such a drift).
//
// Falsification: change CSRFField in tone.go to "csrf_token" -> RED.
func TestCSRFField_MatchesVerifier(t *testing.T) {
	if components.CSRFField != csrf.FormField {
		t.Fatalf("components.CSRFField=%q, csrf.FormField=%q", components.CSRFField, csrf.FormField)
	}
}

// TestQueueRow_EscapesEveryField: every string a consumer feeds in is
// attacker-influenced (lead names, comments). Falsification: render Title (or
// any listed field) through templ.Raw in queuerow.templ -> RED.
func TestQueueRow_EscapesEveryField(t *testing.T) {
	html := render(t, components.QueueRowView(components.QueueRow{
		ID: "r1", Badge: xss, BadgeTone: components.ToneOrange, Meta: xss,
		Title: xss, Subtitle: xss, Secondary: xss,
		Actions: []components.RowAction{{Label: xss, Variant: components.VariantPrimary, Href: "/x"}},
	}))
	mustNotContainRaw(t, html)
	if n := strings.Count(html, "&lt;script&gt;"); n < 6 {
		t.Errorf("want all 6 fields escaped, found %d escaped occurrences:\n%s", n, html)
	}
}

func TestQueueRow_StructureAndBadgeTone(t *testing.T) {
	html := render(t, components.QueueRowView(components.QueueRow{
		ID: "lead-7", Badge: "New", BadgeTone: components.ToneOrange, Meta: "3 h",
		Title: "Anna", Subtitle: "Cafe", Href: "/admin/leads/7", Secondary: "30k",
	}))
	for _, want := range []string{
		`class="queue-row"`, `id="lead-7"`, `class="badge badge-orange"`,
		`class="queue-meta">3 h`, `<a class="queue-title" href="/admin/leads/7">Anna</a>`,
		`class="queue-subtitle">Cafe`, `class="queue-secondary">30k`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "queue-actions") {
		t.Error("no actions given, but .queue-actions rendered")
	}
}

// Falsification: drop the maxQueueActions slice in queueActions -> RED.
func TestQueueRow_CapsActionsAtThree(t *testing.T) {
	acts := []components.RowAction{}
	for _, l := range []string{"A1", "A2", "A3", "A4"} {
		acts = append(acts, components.RowAction{Label: l, Href: "/" + l})
	}
	html := render(t, components.QueueRowView(components.QueueRow{Title: "t", Actions: acts}))
	if !strings.Contains(html, "A3") || strings.Contains(html, "A4") {
		t.Errorf("want exactly 3 actions rendered:\n%s", html)
	}
}

// A link action's Href is consumer data; javascript: must not survive.
// Falsification: wrap the Href in templ.SafeURL(...) in queuerow.templ -> RED.
func TestQueueRow_JavascriptHrefSanitised(t *testing.T) {
	html := render(t, components.QueueRowView(components.QueueRow{
		Title: "t", Href: "javascript:alert(1)",
		Actions: []components.RowAction{{Label: "Go", Href: "javascript:alert(2)"}},
	}))
	if strings.Contains(html, "javascript:") {
		t.Errorf("javascript: URL survived:\n%s", html)
	}
}

func TestQueueRow_PostActionIsRealFormWithToken(t *testing.T) {
	html := render(t, components.QueueRowView(components.QueueRow{
		Title: "t",
		Actions: []components.RowAction{{
			Label: "Take", Variant: components.VariantPrimary,
			Post: &components.PostAction{
				URL: "/admin/leads/7/status", Token: "tok123",
				Fields: []components.HiddenField{{Name: "status", Value: `a"b`}},
			},
		}},
	}))
	for _, want := range []string{
		`<form method="post" action="/admin/leads/7/status"`,
		`hx-post="/admin/leads/7/status"`, `hx-target="closest .queue-row"`, `hx-swap="outerHTML"`,
		`<input type="hidden" name="_csrf" value="tok123">`,
		`name="status" value="a&#34;b"`,
		`<button type="submit" class="btn btn-primary">Take</button>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

// An empty token must render NO _csrf input (the verifier then 403s) rather
// than an empty one. Falsification: drop the `if p.Token != ""` guard -> RED.
func TestPostAction_EmptyTokenOmitsField(t *testing.T) {
	html := render(t, components.QueueRowView(components.QueueRow{
		Title: "t", Actions: []components.RowAction{{Label: "Go", Post: &components.PostAction{URL: "/x"}}},
	}))
	if strings.Contains(html, `name="_csrf"`) {
		t.Errorf("empty token rendered a _csrf input:\n%s", html)
	}
}

func TestQueueList_WrapsRows(t *testing.T) {
	html := render(t, components.QueueList([]components.QueueRow{{Title: "a"}, {Title: "b"}}))
	if strings.Count(html, `class="queue-row"`) != 2 || !strings.Contains(html, `class="queue-list"`) {
		t.Errorf("unexpected list markup:\n%s", html)
	}
}

func TestFunnel_BarsTextAndAria(t *testing.T) {
	html := render(t, components.FunnelView(components.Funnel{
		Bars: []components.FunnelBar{
			{Label: "New", Count: 4, Percent: 27, Tone: components.ToneOrange},
			{Label: "Won", Count: 2, Percent: 13, Tone: components.ToneGreen},
			{Label: "Lost", Count: 0, Percent: 0, Tone: components.ToneGray},
		},
		Note: "Conversion 13%",
	}))
	for _, want := range []string{
		`class="funnel-bar tone-orange" style="width:100%;"`,
		`class="funnel-bar tone-green" style="width:50%;"`,
		`class="funnel-bar tone-gray" style="width:0%;"`,
		`role="img" aria-label="New: 4 (27%)"`,
		`<span class="funnel-count">4</span><span class="funnel-pct">27%</span>`,
		`class="funnel-note">Conversion 13%`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

// A non-zero count must never vanish into a 0-width bar.
func TestFunnel_TinyNonZeroKeepsSliver(t *testing.T) {
	html := render(t, components.FunnelView(components.Funnel{Bars: []components.FunnelBar{
		{Label: "Big", Count: 1000, Tone: components.ToneBlue},
		{Label: "Tiny", Count: 1, Tone: components.ToneRed},
	}}))
	if !strings.Contains(html, `tone-red" style="width:2%;"`) {
		t.Errorf("tiny bar lost its sliver:\n%s", html)
	}
}

// Falsification: use templ.Raw on b.Label in funnel.templ -> RED.
func TestFunnel_EscapesLabelAndNote(t *testing.T) {
	html := render(t, components.FunnelView(components.Funnel{
		Bars: []components.FunnelBar{{Label: xss, Count: 1}}, Note: xss,
	}))
	mustNotContainRaw(t, html)
}

func TestStepper_StatesAndAriaCurrent(t *testing.T) {
	st := components.Stepper{
		Label: "Lead status",
		Steps: []components.Step{
			{Label: "New", State: components.StepDone},
			{Label: "Working", State: components.StepCurrent, Tone: components.TonePurple,
				Post: &components.PostAction{URL: "/s/working", Token: "t"}},
			{Label: "Placed", State: components.StepNext, Post: &components.PostAction{URL: "/s/placed", Token: "t"}},
			{Label: "Later", State: components.StepUpcoming},
		},
		Terminal: &components.Step{Label: "Lost", Post: &components.PostAction{URL: "/s/lost", Token: "t"}},
		Hint:     "Next step: Placed",
	}
	html := render(t, components.StepperView(st))
	for _, want := range []string{
		`role="group" aria-label="Lead status"`,
		`class="stepper-step is-done"`,
		`class="stepper-step is-current tone-purple" aria-current="step"`,
		`class="stepper-step is-next"`,
		`class="stepper-step"`,
		`<span class="stepper-glyph" aria-hidden="true">✓</span>`,
		`class="stepper-state"> (done)`, `class="stepper-state"> (current)`, `class="stepper-state"> (next)`,
		`class="stepper-sep"`,
		`action="/s/lost"`,
		`class="stepper-hint">Next step: Placed`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if n := strings.Count(html, `aria-current="step"`); n != 1 {
		t.Errorf("aria-current on %d steps, want 1", n)
	}
	// Static steps (no Post) are not buttons; posting ones are.
	if n := strings.Count(html, "<button"); n != 3 {
		t.Errorf("want 3 buttons (current, next, terminal), got %d:\n%s", n, html)
	}
}

func TestStepper_LocalisedStateText(t *testing.T) {
	html := render(t, components.StepperView(components.Stepper{
		Labels: components.StepperLabels{Done: "пройдено", Current: "текущий", Next: "дальше"},
		Steps: []components.Step{
			{Label: "A", State: components.StepDone}, {Label: "B", State: components.StepCurrent}, {Label: "C", State: components.StepNext},
		},
	}))
	for _, w := range []string{"(пройдено)", "(текущий)", "(дальше)"} {
		if !strings.Contains(html, w) {
			t.Errorf("missing %q", w)
		}
	}
	if strings.Contains(html, "(done)") {
		t.Error("English default leaked despite Labels override")
	}
}

func TestStepper_EscapesFields(t *testing.T) {
	html := render(t, components.StepperView(components.Stepper{
		Label: xss, Hint: xss,
		Steps:    []components.Step{{Label: xss, Post: &components.PostAction{URL: "/x", Token: xss}}},
		Terminal: &components.Step{Label: xss},
	}))
	mustNotContainRaw(t, html)
}

func TestTimeline_KindsAndMeta(t *testing.T) {
	html := render(t, components.Timeline([]components.TimelineItem{
		{Kind: components.TimelineNote, Label: "Note", Body: "Called", Author: "op", Time: "14 Oct"},
		{Kind: components.TimelineStatus, Label: "Status change",
			From: components.TimelineChip{Label: "New", Tone: components.ToneOrange},
			To:   components.TimelineChip{Label: "Working", Tone: components.ToneBlue}, Time: "13 Oct"},
		{Kind: components.TimelineEvent, Body: "Lead received"},
	}))
	for _, want := range []string{
		`<ol class="timeline">`,
		`class="timeline-item timeline-note"`, `class="timeline-body">Called`,
		`class="timeline-meta">Note · op · 14 Oct`,
		`class="timeline-item timeline-status"`,
		`<span class="badge badge-orange">New</span>`, `<span class="badge badge-blue">Working</span>`,
		`class="timeline-meta">Status change · 13 Oct`,
		`class="timeline-item timeline-event"`, `Lead received`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if strings.Count(html, "timeline-meta") != 2 {
		t.Errorf("event with no label/author/time must render no meta line:\n%s", html)
	}
}

// Falsification: use templ.Raw on it.Body in timeline.templ -> RED.
func TestTimeline_EscapesFields(t *testing.T) {
	html := render(t, components.Timeline([]components.TimelineItem{
		{Kind: components.TimelineNote, Body: xss, Author: xss, Label: xss, Time: xss},
		{Kind: components.TimelineStatus, From: components.TimelineChip{Label: xss}, To: components.TimelineChip{Label: xss}},
	}))
	mustNotContainRaw(t, html)
}

func TestKPIStrip_DeltaIsArrowTextAndColour(t *testing.T) {
	html := render(t, components.KPIStrip([]components.KPI{
		{Label: "Revenue", Value: "184 600", Delta: "12%", Trend: components.TrendUp, Note: "vs Sep", Spark: []int{1, 3, 2, 5}},
		{Label: "Slots", Value: "4 / 24", Delta: "1 slot", Trend: components.TrendDown},
		{Label: "Plain", Value: "7"},
	}))
	for _, want := range []string{
		`class="kpi-strip"`, `class="kpi-label">Revenue`,
		`class="kpi-delta kpi-delta-up"`, `↑12%`, `class="kpi-note"> vs Sep`,
		`class="kpi-delta kpi-delta-down"`, `↓1 slot`, `class="sparkline`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	// The third cell has no delta line at all.
	if strings.Count(html, `class="kpi-delta`) != 2 {
		t.Errorf("TrendNone/no-note cell must render no delta line:\n%s", html)
	}
}

func TestKPIStrip_EscapesFields(t *testing.T) {
	html := render(t, components.KPIStrip([]components.KPI{
		{Label: xss, Value: xss, Delta: xss, Trend: components.TrendUp, Note: xss},
	}))
	mustNotContainRaw(t, html)
}

// An out-of-range Tone must degrade to the neutral look, never emit a stray class.
func TestTone_UnknownFallsBackToNeutral(t *testing.T) {
	html := render(t, components.QueueRowView(components.QueueRow{Badge: "x", BadgeTone: components.Tone(99), Title: "t"}))
	if !strings.Contains(html, `class="badge"`) {
		t.Errorf("unknown tone must render bare .badge:\n%s", html)
	}
}
