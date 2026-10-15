package components

// Tone is a closed set of semantic colours shared by badges, funnel bars,
// stepper steps and timeline chips. Callers pick a Tone; they never pass a CSS
// class, so an open string can never become markup.
type Tone int

const (
	// ToneNeutral is the default: a muted grey chip.
	ToneNeutral Tone = iota
	ToneBlue
	ToneGreen
	ToneRed
	ToneOrange
	TonePurple
	ToneYellow
	ToneGray
)

// toneName maps a Tone to its CSS suffix. Unknown values fall back to
// "neutral" so a bad Tone renders as the quiet default, never as a stray class.
func toneName(t Tone) string {
	switch t {
	case ToneBlue:
		return "blue"
	case ToneGreen:
		return "green"
	case ToneRed:
		return "red"
	case ToneOrange:
		return "orange"
	case TonePurple:
		return "purple"
	case ToneYellow:
		return "yellow"
	case ToneGray:
		return "gray"
	default:
		return "neutral"
	}
}

// toneClass is the shared custom-property class (.tone-*): funnel bar fill,
// stepper current step.
func toneClass(t Tone) string { return "tone-" + toneName(t) }

// badgeClass is the chip class pair for a Tone. ToneNeutral is the bare
// .badge (which is already the neutral look).
func badgeClass(t Tone) string {
	if t == ToneNeutral || toneName(t) == "neutral" {
		return "badge"
	}
	return "badge badge-" + toneName(t)
}

// Variant is a button look: .btn-primary / -secondary / -ghost / -danger.
type Variant int

const (
	VariantSecondary Variant = iota
	VariantPrimary
	VariantGhost
	VariantDanger
)

func btnClass(v Variant) string {
	switch v {
	case VariantPrimary:
		return "btn btn-primary"
	case VariantGhost:
		return "btn btn-ghost"
	case VariantDanger:
		return "btn btn-danger"
	default:
		return "btn btn-secondary"
	}
}

// CSRFField is the hidden-input name the panel's CSRF verifier reads. The
// components package may import only templ and stdlib, so it cannot reference
// csrf.FormField; TestCSRFField_MatchesVerifier pins the two together.
const CSRFField = "_csrf"

// HiddenField is an extra <input type="hidden"> carried by a POST action.
type HiddenField struct {
	Name  string
	Value string
}

// PostAction is a state-changing button rendered as a real <form method=post>
// with hx-post for enhancement. Without JS the browser performs a normal POST
// (the handler answers 303); with htmx the response fragment replaces Target.
//
// Token is the CSRF token (resource.Panel.CSRFToken); an empty Token renders no
// _csrf field, which the verifier rejects with 403 — fail-closed.
type PostAction struct {
	URL    string
	Token  string
	Fields []HiddenField
	// Target / Swap are the htmx hx-target / hx-swap values. Empty Target means
	// "closest .queue-row" for queue rows; callers of other components set it.
	Target string
	Swap   string
}
