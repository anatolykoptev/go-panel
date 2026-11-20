package resource

import (
	"context"
	"html"
	"time"

	"github.com/anatolykoptev/go-panel/i18n"
)

// TimeCell is a list cell showing a moment as relative time ("2 h ago") with the
// absolute time beside it. The relative text is what a reader scans; the
// absolute time (and the exact second, on hover) is what an audit needs. A zero
// time renders an empty cell.
//
// The text is worded by the panel's Strings.RelativeTime, taken from ctx (the
// request context a Lister receives carries them).
func TimeCell(ctx context.Context, t time.Time) Cell {
	return TimeCellAt(ctx, t, time.Now())
}

// TimeCellAt is TimeCell against an explicit "now", for deterministic tests.
func TimeCellAt(ctx context.Context, t, now time.Time) Cell {
	if t.IsZero() {
		return Cell{}
	}
	return Cell{Value: timeHTML(ctx, t, now), HTML: true}
}

// TimeItem is a detail-page item showing a moment the same way TimeCell does.
func TimeItem(ctx context.Context, label string, t time.Time) DetailItem {
	return TimeItemAt(ctx, label, t, time.Now())
}

// TimeItemAt is TimeItem against an explicit "now".
func TimeItemAt(ctx context.Context, label string, t, now time.Time) DetailItem {
	if t.IsZero() {
		return DetailItem{Label: label}
	}
	return DetailItem{Label: label, Value: timeHTML(ctx, t, now), HTML: true}
}

// timeHTML builds the markup. The only input is a time.Time, whose formatted
// parts are digits and zone letters; the relative text goes through
// html.EscapeString because a consumer's Strings.RelativeTime is free text. So
// HTML=true cannot carry consumer markup.
func timeHTML(ctx context.Context, t, now time.Time) string {
	rel := i18n.From(ctx).RelativeTime(t.Sub(now))
	return `<time datetime="` + t.Format(time.RFC3339) + `" title="` + t.Format("2006-01-02 15:04:05 MST") + `">` +
		html.EscapeString(rel) + `</time> <span class="time-abs">` + t.Format("2006-01-02 15:04") + `</span>`
}
