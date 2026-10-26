package components

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
)

// SpanTone is the closed set of occupancy states a bar can be in. Callers pick
// one; the component maps it to a fixed class, a fixed glyph and a text label,
// so colour is never the only carrier.
type SpanTone int

const (
	// SpanSale is a sold (paid or invoiced) lease.
	SpanSale SpanTone = iota
	// SpanHold is a reservation that has not been sold yet.
	SpanHold
	// SpanRenewalDue is a lease that ends soon and has no successor.
	SpanRenewalDue
	// SpanEnded is a finished lease (history).
	SpanEnded
	// SpanConflict is a span that overlaps another one and needs attention.
	SpanConflict
)

func spanToneName(t SpanTone) string {
	switch t {
	case SpanHold:
		return "hold"
	case SpanRenewalDue:
		return "renewal"
	case SpanEnded:
		return "ended"
	case SpanConflict:
		return "conflict"
	default:
		return "sale"
	}
}

// spanGlyph is a non-colour marker so tones differ in shape too.
func spanGlyph(t SpanTone) string {
	switch t {
	case SpanHold:
		return "◌"
	case SpanRenewalDue:
		return "↻"
	case SpanEnded:
		return "–"
	case SpanConflict:
		return "!"
	default:
		return "●"
	}
}

// spanBadgeTone maps a SpanTone onto the shared Tone set for the mobile list.
func spanBadgeTone(t SpanTone) Tone {
	switch t {
	case SpanHold:
		return ToneYellow
	case SpanRenewalDue:
		return ToneOrange
	case SpanEnded:
		return ToneGray
	case SpanConflict:
		return ToneRed
	default:
		return ToneGreen
	}
}

// Span is one booked interval in one row. From is inclusive and To exclusive
// (half-open), both calendar dates: only the year, month and day of each are
// used, the clock and zone are ignored. A Span with To <= From has no extent
// and is ignored. Href is optional; Label is the visible text of the bar.
type Span struct {
	Row   int
	From  time.Time
	To    time.Time
	Tone  SpanTone
	Label string
	Href  string
}

// OccupancyRow is one labelled row (a position). Num is the key Span.Row and
// the {row} placeholder refer to.
type OccupancyRow struct {
	Num   int
	Label string
}

// OccupancyLabels is the visible and screen-reader text. Zero fields fall back
// to English. FormatDate renders one calendar date (default ISO 2006-01-02).
// Months, when set, replaces the three-letter English month names in the
// header.
type OccupancyLabels struct {
	Sale, Hold, RenewalDue, Ended, Conflict         string
	Free, Today, Overlaps, Zoom, MonthsUnit, Legend string
	FormatDate                                      func(time.Time) string
	Months                                          [12]string
}

// Occupancy is the input of OccupancyBoard. The component never reads the
// wall clock: Today and the window Start are inputs.
//
//   - Rows are the positions, in display order. A span whose Row is not listed
//     gets an extra row appended (labelled with its number) rather than being
//     dropped.
//   - Start's month is the first month shown; Months (6, 12 or 18, anything
//     else means 12) is how many months the window covers.
//   - Today draws the today line when inside the window (zero Time: no line).
//     Free gaps are only offered from Today on; with a zero Today, from the
//     window start.
//   - GapHref is the free-gap link template; {from}, {to} (exclusive) and
//     {row} are replaced by YYYY-MM-DD, YYYY-MM-DD and the row number. Empty:
//     gaps are shown but are not links.
//   - ZoomHref is the zoom link template with a {months} placeholder; empty:
//     no zoom control.
//
// Every span, whatever its tone (Ended included), blocks a gap: pass only
// spans that really occupy the position. The zero SpanTone is SpanSale. The
// mobile list shows every span passed in, also those outside the window, so
// the caller bounds the set (e.g. drop leases that ended long ago).
//
// Every span is drawn: overlapping spans in one row are stacked in lanes and
// flagged, never hidden; a span crossing the window edge is clipped, not
// dropped. Gaps are the days of the window no span covers (any tone covers,
// including holds, so a drawn gap is always free to book).
type Occupancy struct {
	Label    string
	Rows     []OccupancyRow
	Spans    []Span
	Today    time.Time
	Start    time.Time
	Months   int
	GapHref  string
	ZoomHref string
	Labels   OccupancyLabels
}

// ---- date arithmetic: whole days since 1970-01-01, zone-free -----------

func dayNum(t time.Time) int {
	y, m, d := t.Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / secondsPerDay)
}

func dayTime(n int) time.Time { return time.Unix(int64(n)*secondsPerDay, 0).UTC() }

const (
	isoDate       = "2006-01-02"
	secondsPerDay = 86400
	zoomShort     = 6
	zoomDefault   = 12
	zoomLong      = 18
	// laneMarginRem is the track padding under the last lane.
	laneMarginRem = 0.25
)

func occZoom(m int) int {
	switch m {
	case zoomShort, zoomDefault, zoomLong:
		return m
	default:
		return zoomDefault
	}
}

// occWindow returns the window [from,to) in day numbers.
func occWindow(o Occupancy) (from, to int) {
	y, m, _ := o.Start.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return dayNum(first), dayNum(first.AddDate(0, occZoom(o.Months), 0))
}

// occGap is a free half-open day range.
type occGap struct{ From, To int }

// occGaps returns the days in [lo,hi) not covered by any span, as maximal
// half-open ranges: adjacent spans leave no gap, a one-day hole is a gap.
func occGaps(spans []Span, lo, hi int) []occGap {
	type iv struct{ f, t int }
	ivs := make([]iv, 0, len(spans))
	for _, s := range spans {
		if f, t := dayNum(s.From), dayNum(s.To); t > f {
			ivs = append(ivs, iv{f, t})
		}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].f < ivs[j].f })
	var gaps []occGap
	cur := lo
	for _, v := range ivs {
		if v.f > cur {
			gaps = append(gaps, occGap{cur, min(v.f, hi)})
		}
		cur = max(cur, v.t)
		if cur >= hi {
			return gaps
		}
	}
	if cur < hi {
		gaps = append(gaps, occGap{cur, hi})
	}
	return gaps
}

// ---- view model --------------------------------------------------------

type occBar struct {
	Span       Span
	Lane       int
	Left, Wide float64 // percent of the window
	ClipL      bool
	ClipR      bool
	Overlap    bool
	Visible    bool // intersects the window
}

type occGapView struct {
	occGap
	Left, Wide float64
	Href       string
}

type occRowView struct {
	Row   OccupancyRow
	Bars  []occBar
	Gaps  []occGapView
	Lanes int
	Items []occItem // mobile list: spans and gaps in date order
}

// occItem is one line of the per-position list.
type occItem struct {
	Span    *Span
	Overlap bool
	Gap     *occGapView
	From    int
}

type occMonth struct {
	Name string
	Days int
}

type occView struct {
	Rows      []occRowView
	Months    []occMonth
	Cols      string // grid-template-columns of the month header
	TodayLeft float64
	HasToday  bool
	Zoom      int
}

func pct(n, total int) float64 { return float64(n) * 100 / float64(total) }

func (o Occupancy) expandGap(g occGap, row int) string {
	if o.GapHref == "" {
		return ""
	}
	return strings.NewReplacer(
		"{from}", dayTime(g.From).Format(isoDate),
		"{to}", dayTime(g.To).Format(isoDate),
		"{row}", strconv.Itoa(row),
	).Replace(o.GapHref)
}

func (o Occupancy) rowList() []OccupancyRow {
	rows := append([]OccupancyRow(nil), o.Rows...)
	known := map[int]bool{}
	for _, r := range rows {
		known[r.Num] = true
	}
	var extra []int
	for _, s := range o.Spans {
		if !known[s.Row] {
			known[s.Row] = true
			extra = append(extra, s.Row)
		}
	}
	sort.Ints(extra)
	for _, n := range extra {
		rows = append(rows, OccupancyRow{Num: n, Label: strconv.Itoa(n)})
	}
	return rows
}

func (o Occupancy) view() occView {
	lo, hi := occWindow(o)
	total := hi - lo
	zoom := occZoom(o.Months)
	v := occView{Zoom: zoom}

	y, m, _ := o.Start.Date()
	var cols []string
	for i := 0; i < zoom; i++ {
		d := time.Date(y, m+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		days := dayNum(d.AddDate(0, 1, 0)) - dayNum(d)
		v.Months = append(v.Months, occMonth{Name: o.Labels.monthName(d), Days: days})
		cols = append(cols, strconv.Itoa(days)+"fr")
	}
	v.Cols = strings.Join(cols, " ")

	if !o.Today.IsZero() {
		if t := dayNum(o.Today); t >= lo && t < hi {
			v.HasToday, v.TodayLeft = true, pct(t-lo, total)
		}
	}
	gapFrom := lo
	if !o.Today.IsZero() {
		gapFrom = max(lo, dayNum(o.Today))
	}

	for _, r := range o.rowList() {
		v.Rows = append(v.Rows, o.rowView(r, lo, hi, gapFrom))
	}
	return v
}

// rowView lays out one row: lane-stacked bars, free gaps and the list items.
func (o Occupancy) rowView(r OccupancyRow, lo, hi, gapFrom int) occRowView {
	total := hi - lo
	var spans []Span
	for _, s := range o.Spans {
		if s.Row == r.Num && dayNum(s.To) > dayNum(s.From) {
			spans = append(spans, s)
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if a, b := dayNum(spans[i].From), dayNum(spans[j].From); a != b {
			return a < b
		}
		return dayNum(spans[i].To) < dayNum(spans[j].To)
	})
	rv := occRowView{Row: r}
	var laneEnd []int
	for i, s := range spans {
		f, t := dayNum(s.From), dayNum(s.To)
		b := occBar{Span: s, Overlap: occOverlaps(spans, i), Visible: t > lo && f < hi}
		if b.Visible { // history outside the window must not grow the track
			b.Lane = occLane(&laneEnd, f, t)
		}
		if b.Visible {
			cf, ct := max(f, lo), min(t, hi)
			b.Left, b.Wide = pct(cf-lo, total), pct(ct-cf, total)
			b.ClipL, b.ClipR = f < lo, t > hi
		}
		rv.Bars = append(rv.Bars, b)
	}
	rv.Lanes = max(1, len(laneEnd))
	if gapFrom < hi {
		for _, g := range occGaps(spans, gapFrom, hi) {
			rv.Gaps = append(rv.Gaps, occGapView{
				occGap: g, Left: pct(g.From-lo, total), Wide: pct(g.To-g.From, total),
				Href: o.expandGap(g, r.Num),
			})
		}
	}
	for i := range rv.Bars {
		rv.Items = append(rv.Items, occItem{Span: &rv.Bars[i].Span, Overlap: rv.Bars[i].Overlap, From: dayNum(rv.Bars[i].Span.From)})
	}
	for i := range rv.Gaps {
		rv.Items = append(rv.Items, occItem{Gap: &rv.Gaps[i], From: rv.Gaps[i].From})
	}
	sort.SliceStable(rv.Items, func(i, j int) bool { return rv.Items[i].From < rv.Items[j].From })
	return rv
}

// occLane picks the first lane free at day f (first-fit) and books it to t.
func occLane(laneEnd *[]int, f, t int) int {
	for l, e := range *laneEnd {
		if e <= f {
			(*laneEnd)[l] = t
			return l
		}
	}
	*laneEnd = append(*laneEnd, t)
	return len(*laneEnd) - 1
}

// occOverlaps reports whether spans[i] shares a day with any other span.
func occOverlaps(spans []Span, i int) bool {
	f, t := dayNum(spans[i].From), dayNum(spans[i].To)
	for j, q := range spans {
		if j != i && dayNum(q.From) < t && f < dayNum(q.To) {
			return true
		}
	}
	return false
}

// ---- text --------------------------------------------------------------

func pick(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

func (l OccupancyLabels) monthName(d time.Time) string {
	n := l.Months[d.Month()-1]
	if n == "" {
		n = d.Month().String()[:3]
	}
	return n + " " + strconv.Itoa(d.Year())
}

func (l OccupancyLabels) toneLabel(t SpanTone) string {
	switch t {
	case SpanHold:
		return pick(l.Hold, "Hold")
	case SpanRenewalDue:
		return pick(l.RenewalDue, "Renewal due")
	case SpanEnded:
		return pick(l.Ended, "Ended")
	case SpanConflict:
		return pick(l.Conflict, "Conflict")
	default:
		return pick(l.Sale, "Sale")
	}
}

func (l OccupancyLabels) date(d time.Time) string {
	if l.FormatDate != nil {
		return l.FormatDate(d)
	}
	return d.Format(isoDate)
}

// occRange prints [from,to) as "first day – last day" (the exclusive end is
// shown as the last occupied day, which is how people read it).
func (l OccupancyLabels) rng(from, to int) string {
	last := to - 1
	if last < from {
		last = from
	}
	return l.date(dayTime(from)) + " – " + l.date(dayTime(last))
}

// spanDetail is the tone and range without the label (the bar already shows
// the label, so repeating it would make the accessible name read twice).
func (l OccupancyLabels) spanDetail(s Span) string {
	return l.toneLabel(s.Tone) + ", " + l.rng(dayNum(s.From), dayNum(s.To))
}

func (l OccupancyLabels) spanText(s Span) string {
	t := l.toneLabel(s.Tone) + ", " + l.rng(dayNum(s.From), dayNum(s.To))
	if s.Label != "" {
		t = s.Label + ": " + t
	}
	return t
}

func (l OccupancyLabels) gapText(g occGap) string {
	return pick(l.Free, "Free") + ", " + l.rng(g.From, g.To)
}

func occBarClass(b occBar) string {
	c := "occ-bar occ-" + spanToneName(b.Span.Tone)
	if b.Overlap {
		c += " is-overlap"
	}
	if b.ClipL {
		c += " is-clip-l"
	}
	if b.ClipR {
		c += " is-clip-r"
	}
	return c
}

func occBarStyle(b occBar) templ.SafeCSS {
	return templ.SafeCSS("left:" + pctStr(b.Left) + ";width:" + pctStr(b.Wide) + ";top:" + remStr(float64(b.Lane)*occLaneRem))
}

func occGapStyle(g occGapView) templ.SafeCSS {
	return templ.SafeCSS("left:" + pctStr(g.Left) + ";width:" + pctStr(g.Wide))
}

// occLaneRem is the height of one stacked lane; keep in step with .occ-bar in
// shell/styles.templ (lane height minus .25rem of margin).
const occLaneRem = 1.875

func remStr(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) + "rem" }

func pctStr(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) + "%" }

func occTrackStyle(lanes int) templ.SafeCSS {
	return templ.SafeCSS("height:" + remStr(float64(lanes)*occLaneRem+laneMarginRem))
}

func occGridStyle(cols string) templ.SafeCSS {
	return templ.SafeCSS("grid-template-columns:" + cols)
}

func occTodayStyle(left float64) templ.SafeCSS { return templ.SafeCSS("left:" + pctStr(left)) }

func occZoomHref(tpl string, months int) string {
	return strings.ReplaceAll(tpl, "{months}", strconv.Itoa(months))
}

func itoa(n int) string { return strconv.Itoa(n) }
