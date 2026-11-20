package i18n

import "time"

// RelUnit is the coarse unit Relative reduces a duration to.
type RelUnit int

// The units Relative returns.
const (
	RelNow RelUnit = iota // under 45 seconds either way
	RelMinute
	RelHour
	RelDay
)

// Relative reduces d (negative = in the past) to a count and a unit, rounded
// down, with "now" under 45 seconds. Language presets build their
// Strings.RelativeTime from it, so the bucketing is one rule and only the words
// differ per language.
func Relative(d time.Duration) (n int, u RelUnit, future bool) {
	future = d > 0
	if d < 0 {
		d = -d
	}
	switch {
	case d < 45*time.Second:
		return 0, RelNow, future
	case d < time.Hour:
		return max(1, int(d/time.Minute)), RelMinute, future
	case d < 24*time.Hour:
		return int(d / time.Hour), RelHour, future
	default:
		return int(d / (24 * time.Hour)), RelDay, future
	}
}
