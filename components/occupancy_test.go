package components_test

import (
	"regexp"
	"strconv"
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

// A span nested inside a longer one must not end the longer one's cover.
// Mutation: occupancy.go occGaps `cur = max(cur, v.t)` -> `cur = v.t` -> RED
// (the sale's days after the hold would be offered as free).
func TestOccupancy_NestedSpanDoesNotOpenGap(t *testing.T) {
	o := occBase()
	o.Spans = []components.Span{
		{Row: 1, From: day(2026, 1, 1), To: day(2026, 6, 1), Tone: components.SpanSale, Label: "Sale"},
		{Row: 1, From: day(2026, 2, 1), To: day(2026, 3, 1), Tone: components.SpanHold, Label: "Hold"},
	}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	var got []string
	for _, l := range gapLinks(board) {
		if strings.HasSuffix(l, "row=1") {
			got = append(got, l)
		}
	}
	if want := "/book?from=2026-06-01&to=2026-07-01&row=1"; len(got) != 1 || got[0] != want {
		t.Fatalf("row 1 gaps = %v, want only [%s]", got, want)
	}
}

// Only the calendar date of each time counts, in the time's own zone.
// Mutation: occupancy.go dayNum `t.Date()` -> `t.UTC().Date()` -> RED.
func TestOccupancy_DatePartOnly(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	ny := time.FixedZone("EST", -5*3600)
	o := occBase()
	o.Today = time.Date(2026, 3, 15, 23, 30, 0, 0, ny) // 04:30Z on the 16th
	o.Spans = []components.Span{{
		Row: 1, Label: "M", From: time.Date(2026, 2, 1, 0, 0, 0, 0, msk), // 21:00Z on 31 Jan
		To: time.Date(2026, 3, 1, 0, 0, 0, 0, msk),
	}}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	if !strings.Contains(board, "Sale, 2026-02-01 – 2026-02-28") {
		t.Errorf("span dates shifted:\n%s", board)
	}
	if !strings.Contains(board, "from=2026-03-15&amp;to=2026-07-01&amp;row=1") {
		t.Errorf("today shifted, gap should start 2026-03-15:\n%s", board)
	}
}

// History outside the window must not stack extra empty lanes.
// Mutation: occupancy.go rowView drop the `if b.Visible {` guard around
// occLane -> RED.
func TestOccupancy_LanesCountOnlyVisibleSpans(t *testing.T) {
	o := occBase()
	o.Rows = o.Rows[:1] // one track only, so the assertion cannot be met by another row
	o.Spans = []components.Span{
		{Row: 1, From: day(2020, 1, 1), To: day(2020, 12, 1), Tone: components.SpanEnded, Label: "h1"},
		{Row: 1, From: day(2020, 6, 1), To: day(2021, 6, 1), Tone: components.SpanEnded, Label: "h2"},
	}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	if !strings.Contains(board, `style="height:2.125rem;"`) {
		t.Errorf("empty track should be one lane high:\n%s", board)
	}
}

// Non-link bars/gaps are not tab stops; the label is not read twice; the
// legend name is localisable.
func TestOccupancy_NonLinksNotFocusableAndNameNotDuplicated(t *testing.T) {
	o := occBase()
	o.Labels.Legend = "Легенда"
	o.Spans = []components.Span{{Row: 1, From: day(2026, 2, 1), To: day(2026, 3, 1), Label: "Atlas"}}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	if strings.Contains(board, "tabindex") {
		t.Errorf("non-link element is a tab stop")
	}
	if strings.Contains(board, "Atlas: Sale") || strings.Contains(board, "Atlas</span>Atlas") {
		t.Errorf("label repeated in the accessible text:\n%s", board)
	}
	if !strings.Contains(board, `aria-label="Легенда"`) {
		t.Errorf("legend label not taken from Labels.Legend")
	}
}

var (
	reBar  = regexp.MustCompile(`class="occ-bar[^"]*" style="left:([\d.]+)%;width:([\d.]+)%;top:([\d.]+)rem;"`)
	reMark = regexp.MustCompile(`class="occ-mark" style="left:([\d.]+)%;margin-left:-0.75rem;width:1.5rem;top:([\d.]+)rem;" href="([^"]+)"`)
	reGapA = regexp.MustCompile(`<a class="occ-gap" style="left:([\d.]+)%;width:([\d.]+)%;`)
)

func atof(t *testing.T, s string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// A short gap's link must never sit on a bar: it is a marker in a strip below
// the lanes, and no in-lane gap link intersects a bar's box. Gaps of 1..8 days,
// next to a stacked (two-lane) neighbour.
// Mutation: occupancy.go occShortGapDays `8` -> `0` (short gaps become in-lane
// links again, as before the fix) -> RED.
func TestOccupancy_ShortGapLinkNeverOverlapsBars(t *testing.T) {
	for n := 1; n <= 8; n++ {
		o := occBase()
		o.Rows = o.Rows[:1]
		end := day(2026, 3, 1)
		o.Spans = []components.Span{
			{Row: 1, From: day(2026, 1, 1), To: end, Label: "A"},
			{Row: 1, From: day(2026, 1, 10), To: end, Label: "A2"}, // overlaps: two lanes
			{Row: 1, From: end.AddDate(0, 0, n), To: day(2026, 7, 1), Label: "B"},
		}
		board, _ := split(t, render(t, components.OccupancyBoard(o)))
		bars := reBar.FindAllStringSubmatch(board, -1)
		if len(bars) != 3 {
			t.Fatalf("n=%d: want 3 bars, got %d", n, len(bars))
		}
		marks := reMark.FindAllStringSubmatch(board, -1)
		if len(marks) != 1 {
			t.Fatalf("n=%d: want exactly one marker for the %d-day gap, got %d\n%s", n, n, len(marks), board)
		}
		// 2 lanes of 1.875rem + .25rem margin: the marker strip starts at 4rem.
		if top := atof(t, marks[0][2]); top < 4 {
			t.Errorf("n=%d: marker top %.3frem is inside the lanes (bars end at 4rem)", n, top)
		}
		for _, g := range reGapA.FindAllStringSubmatch(board, -1) {
			gl, gw := atof(t, g[1]), atof(t, g[2])
			for _, b := range bars {
				bl, bw := atof(t, b[1]), atof(t, b[2])
				if gl < bl+bw && bl < gl+gw {
					t.Errorf("n=%d: in-lane gap link [%.2f,%.2f] intersects bar [%.2f,%.2f]", n, gl, gl+gw, bl, bl+bw)
				}
			}
		}
		if len(reGapA.FindAllString(board, -1)) != 0 {
			t.Errorf("n=%d: short gap rendered as an in-lane link", n)
		}
		if !strings.Contains(marks[0][3], "row=1") {
			t.Errorf("n=%d: marker href %q lacks the gap range", n, marks[0][3])
		}
	}
}

// Linked bars carry the full label and range as a tooltip; non-links do not
// get a title and stay non-focusable.
func TestOccupancy_TooltipOnLinkedBarsOnly(t *testing.T) {
	o := occBase()
	o.Spans = []components.Span{
		{Row: 1, From: day(2026, 2, 1), To: day(2026, 3, 1), Label: "Linked", Href: "/l"},
		{Row: 1, From: day(2026, 4, 1), To: day(2026, 5, 1), Label: "Plain"},
	}
	board, _ := split(t, render(t, components.OccupancyBoard(o)))
	if !strings.Contains(board, `title="Linked: Sale, 2026-02-01 – 2026-02-28"`) {
		t.Errorf("linked bar lacks its tooltip:\n%s", board)
	}
	if strings.Contains(board, "title=") && strings.Count(board, "title=") != 1 {
		t.Errorf("non-link bar got a title")
	}
	if strings.Contains(board, "tabindex") {
		t.Errorf("tab stop on a non-link")
	}
}
