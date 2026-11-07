package components

import (
	"strconv"
	"strings"
)

// UnitTone is the closed set of states a numbered unit can be in. Callers pick
// one; the component maps it to a fixed class, a fixed glyph and a text label,
// so colour is never the only carrier. The zero value is UnitFree. A value
// outside the set renders as UnitUnverified, never as free: a unit nobody can
// classify must not read as bookable.
type UnitTone int

const (
	// UnitFree is a unit nobody holds: neutral, outlined.
	UnitFree UnitTone = iota
	// UnitHold is a reservation that is not sold yet (dashed, like OccupancyBoard's hold).
	UnitHold
	// UnitSale is a sold (paid or invoiced) unit.
	UnitSale
	// UnitRenewalDue is a sold unit that ends soon and has no successor.
	UnitRenewalDue
	// UnitUnverified is a unit whose state is under review.
	UnitUnverified
	// UnitConflict is a unit with overlapping claims that needs attention.
	UnitConflict
)

// unitTones is the legend order.
var unitTones = []UnitTone{UnitFree, UnitHold, UnitSale, UnitRenewalDue, UnitUnverified, UnitConflict}

func unitToneName(t UnitTone) string {
	switch t {
	case UnitFree:
		return "free"
	case UnitHold:
		return "hold"
	case UnitSale:
		return "sale"
	case UnitRenewalDue:
		return "renewal"
	case UnitConflict:
		return "conflict"
	default:
		return "unverified"
	}
}

// unitGlyph is a non-colour marker so tones differ in shape too. The glyphs of
// the tones OccupancyBoard also has (hold, sale, renewal, conflict) are the
// same ones, via spanGlyph.
func unitGlyph(t UnitTone) string {
	switch t {
	case UnitFree:
		return "○"
	case UnitHold:
		return spanGlyph(SpanHold)
	case UnitSale:
		return spanGlyph(SpanSale)
	case UnitRenewalDue:
		return spanGlyph(SpanRenewalDue)
	case UnitConflict:
		return spanGlyph(SpanConflict)
	default:
		return "?"
	}
}

// Unit is one numbered tile. Note is short visible text ("until 12.11") and
// Title is the tooltip and accessible description ("Place 3 · Hold until
// 12.11"); both are plain text and always escaped. Href is optional and is
// followed only when it is a same-origin path (see safeUnitHref); anything
// else renders the tile without a link. Empty Title falls back to
// "Place N · tone · note".
type Unit struct {
	Num   int
	Tone  UnitTone
	Note  string
	Title string
	Href  string
}

// UnitLabels is the visible and screen-reader text. Zero fields fall back to
// English.
type UnitLabels struct {
	Place, Legend                                       string
	Free, Hold, Sale, RenewalDue, Unverified, Conflict string
}

// UnitRow is the input of UnitStrip: the tiles of one row of a list. Label
// names the row for screen readers (e.g. the collection name).
type UnitRow struct {
	Label  string
	Units  []Unit
	Labels UnitLabels
}

func (l UnitLabels) toneLabel(t UnitTone) string {
	switch t {
	case UnitFree:
		return pick(l.Free, "Free")
	case UnitHold:
		return pick(l.Hold, "Hold")
	case UnitSale:
		return pick(l.Sale, "Sold")
	case UnitRenewalDue:
		return pick(l.RenewalDue, "Renewal due")
	case UnitConflict:
		return pick(l.Conflict, "Conflict")
	default:
		return pick(l.Unverified, "Under review")
	}
}

// tooltip is the title attribute and the screen-reader name of a tile.
func (l UnitLabels) tooltip(u Unit) string {
	if u.Title != "" {
		return u.Title
	}
	parts := []string{pick(l.Place, "Place") + " " + strconv.Itoa(u.Num), l.toneLabel(u.Tone)}
	if u.Note != "" {
		parts = append(parts, u.Note)
	}
	return strings.Join(parts, " · ")
}

// safeUnitHref reports whether h may be rendered as a link: a same-origin
// path, i.e. it starts with a single "/" (not "//", which is protocol-relative,
// and not "/\", which browsers read as "//") and carries no control character
// (browsers strip tab/CR/LF inside a URL, so "/\t/evil" would become "//evil").
// Href is consumer data; the check lives here so every caller is covered.
func safeUnitHref(h string) bool {
	if len(h) == 0 || h[0] != '/' {
		return false
	}
	if len(h) > 1 && (h[1] == '/' || h[1] == '\\') {
		return false
	}
	for i := 0; i < len(h); i++ {
		if h[i] < ' ' || h[i] == 0x7f {
			return false
		}
	}
	return true
}

func unitClass(t UnitTone) string { return "unit-tile unit-" + unitToneName(t) }
