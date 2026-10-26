package components_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/components"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

const gapTpl = "/book?from={from}&to={to}&row={row}"

func occBase() components.Occupancy {
	return components.Occupancy{
		Label:   "Occupancy",
		Rows:    []components.OccupancyRow{{Num: 1, Label: "Position 1"}, {Num: 2, Label: "Position 2"}},
		Start:   day(2026, 1, 1),
		Months:  6,
		Today:   day(2026, 1, 1),
		GapHref: gapTpl,
	}
}

// split returns the desktop board markup and the mobile list markup.
func split(t *testing.T, html string) (board, list string) {
	t.Helper()
	i := strings.Index(html, `<div class="occ-list">`)
	if i < 0 {
		t.Fatalf("no occ-list in output:\n%s", html)
	}
	return html[:i], html[i:]
}

func gapLinks(s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`href="(/book\?[^"]+)"`).FindAllStringSubmatch(s, -1) {
		out = append(out, strings.ReplaceAll(m[1], "&amp;", "&"))
	}
	return out
}

// F1: half-open gaps. Adjacent spans leave no gap; a one-day hole is a gap.
// Mutation: occupancy.go occGaps `if v.f > cur` -> `if v.f >= cur` (adjacent
// spans then yield an empty gap link) -> RED.
func TestOccupancy_F1_GapsAreHalfOpenAndExact(t *testing.T) {
	o := occBase()
	o.Spans = []components.Span{
		{Row: 1, From: day(2026, 1, 1), To: day(2026, 3, 1), Label: "A"},
		{Row: 1, From: day(2026, 3, 1), To: day(2026, 4, 10), Label: "B"}, // adjacent to A
		{Row: 1, From: day(2026, 4, 11), To: day(2026, 7, 1), Label: "C"}, // one-day hole 10 Apr
	}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	got := []string{}
	for _, l := range gapLinks(board) {
		if strings.HasSuffix(l, "row=1") {
			got = append(got, l)
		}
	}
	want := []string{"/book?from=2026-04-10&to=2026-04-11&row=1"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("row 1 gaps = %v, want %v", got, want)
	}
}

// F2: a span crossing the window is clipped, not dropped.
// Mutation: occupancy.go rowView `Visible: t > lo && f < hi` -> `Visible: f >= lo && t <= hi` -> RED.
func TestOccupancy_F2_SpanCrossingWindowIsClipped(t *testing.T) {
	o := occBase() // window 2026-01-01 .. 2026-07-01
	o.Spans = []components.Span{
		{Row: 1, From: day(2025, 11, 1), To: day(2026, 2, 1), Label: "EarlyCross"},
		{Row: 2, From: day(2026, 6, 1), To: day(2027, 1, 1), Label: "LateCross"},
		{Row: 2, From: day(2024, 1, 1), To: day(2024, 6, 1), Label: "FarPast"},
	}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	for _, l := range []string{"EarlyCross", "LateCross"} {
		if !strings.Contains(board, l) {
			t.Errorf("clipped span %q dropped from board", l)
		}
	}
	if !strings.Contains(board, "is-clip-l") || !strings.Contains(board, "is-clip-r") {
		t.Errorf("clip flags missing:\n%s", board)
	}
	if strings.Contains(board, "FarPast") {
		t.Errorf("span outside the window should not be drawn on the board")
	}
	// Early span starts at window start: left:0.000%.
	if !strings.Contains(board, "left:0.000%") {
		t.Errorf("early span not clipped to window start")
	}
}

// F3: overlap in one row stays visible, in separate lanes, flagged.
// Mutation: occupancy.go occLane `if e <= f` -> `if e <= f+100000` (every
// span lands in lane 0, hidden under the other) -> RED.
func TestOccupancy_F3_OverlapStaysVisible(t *testing.T) {
	o := occBase()
	o.Spans = []components.Span{
		{Row: 1, From: day(2026, 2, 1), To: day(2026, 5, 1), Tone: components.SpanHold, Label: "HoldX"},
		{Row: 1, From: day(2026, 3, 1), To: day(2026, 6, 1), Tone: components.SpanSale, Label: "SaleY"},
	}
	board, list := split(t, render(t, components.OccupancyBoard(o)))
	for _, l := range []string{"HoldX", "SaleY"} {
		if !strings.Contains(board, l) {
			t.Errorf("%s hidden on board", l)
		}
	}
	if n := strings.Count(board, "is-overlap"); n != 2 {
		t.Errorf("want both spans flagged is-overlap, got %d", n)
	}
	if !strings.Contains(board, "top:0rem") || !strings.Contains(board, "top:1.875rem") {
		t.Errorf("overlapping spans not stacked in two lanes:\n%s", board)
	}
	if !strings.Contains(list, "Overlaps another span") {
		t.Errorf("overlap not flagged in the list")
	}
}

// F4: the today line comes from the input, never the wall clock.
// Mutation: occupancy.go view `dayNum(o.Today); t >= lo` -> `dayNum(time.Now()); t >= lo` -> RED.
func TestOccupancy_F4_TodayLineFromInput(t *testing.T) {
	o := occBase()
	o.Today = day(2026, 4, 1) // 90 of 181 days in
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	if !strings.Contains(board, `class="occ-today" style="left:49.724%;"`) {
		t.Errorf("today line not at 90/181 of the window:\n%s", board)
	}
	o.Today = day(2031, 1, 1) // outside the window: no line
	board, _ = split(t, render(t, components.OccupancyBoard(o)))
	if strings.Contains(board, `class="occ-today"`) {
		t.Errorf("today line drawn for a date outside the window")
	}
	o.Today = time.Time{} // zero: no line, gaps from window start
	board, _ = split(t, render(t, components.OccupancyBoard(o)))
	if strings.Contains(board, `class="occ-today"`) {
		t.Errorf("today line drawn for a zero Today")
	}
}

// F5: the mobile list holds every span (even outside the window) and every gap.
// Mutation: occupancy.go rowView drop the gap items loop
// (`for i := range rv.Gaps { rv.Items = append(...) }`) -> RED.
func TestOccupancy_F5_MobileListHasEverySpanAndGap(t *testing.T) {
	o := occBase()
	o.Spans = []components.Span{
		{Row: 1, From: day(2026, 2, 1), To: day(2026, 3, 1), Label: "S1", Href: "/l/1"},
		{Row: 1, From: day(2026, 4, 1), To: day(2026, 5, 1), Label: "S2"},
		{Row: 2, From: day(2020, 1, 1), To: day(2020, 6, 1), Label: "OldOne", Tone: components.SpanEnded},
		{Row: 9, From: day(2026, 2, 1), To: day(2026, 3, 1), Label: "Stray"}, // unlisted row
	}
	_, list := split(t, render(t, components.OccupancyBoard(o)))
	for _, l := range []string{"S1", "S2", "OldOne", "Stray", "Ended"} {
		if !strings.Contains(list, l) {
			t.Errorf("list lacks %q", l)
		}
	}
	// Row 1 gaps: Jan1-Feb1, Mar1-Apr1, May1-Jul1.
	for _, g := range []string{
		"/book?from=2026-01-01&to=2026-02-01&row=1",
		"/book?from=2026-03-01&to=2026-04-01&row=1",
		"/book?from=2026-05-01&to=2026-07-01&row=1",
		"/book?from=2026-01-01&to=2026-07-01&row=2",
	} {
		found := false
		for _, l := range gapLinks(list) {
			found = found || l == g
		}
		if !found {
			t.Errorf("list lacks gap link %s (have %v)", g, gapLinks(list))
		}
	}
}

func TestOccupancy_PastIsNotOfferedAndToneTextPresent(t *testing.T) {
	o := occBase()
	o.Today = day(2026, 3, 15)
	o.Spans = []components.Span{{Row: 1, From: day(2026, 2, 1), To: day(2026, 3, 1), Tone: components.SpanConflict, Label: "X"}}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	if strings.Contains(board, "from=2026-01-01") {
		t.Errorf("gap offered in the past")
	}
	if !strings.Contains(board, "from=2026-03-15&amp;to=2026-07-01&amp;row=1") {
		t.Errorf("gap does not start at today:\n%s", board)
	}
	if !strings.Contains(board, "Conflict, 2026-02-01 – 2026-02-28") {
		t.Errorf("tone not carried as text:\n%s", board)
	}
}

func TestOccupancy_ZoomAndEscaping(t *testing.T) {
	o := occBase()
	o.Months = 18
	o.ZoomHref = "?zoom={months}"
	o.Spans = []components.Span{{Row: 1, From: day(2026, 2, 1), To: day(2026, 3, 1), Label: xss, Href: "javascript:alert(1)"}}
	html := render(t, components.OccupancyBoard(o))
	mustNotContainRaw(t, html)
	if strings.Contains(html, `href="javascript:`) {
		t.Errorf("unsafe URL emitted")
	}
	if !strings.Contains(html, `aria-current="true" href="?zoom=18"`) || !strings.Contains(html, `href="?zoom=6"`) {
		t.Errorf("zoom links wrong:\n%s", html)
	}
	if n := strings.Count(html, `<i></i>`); n != 18*2 {
		t.Errorf("want 18 month cells per row, got %d total", n)
	}
}
