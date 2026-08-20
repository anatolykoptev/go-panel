package resource

import (
	"strings"
	"testing"
)

// TestSanitizeForLog_StripsControlChars verifies that newlines, carriage
// returns, and ANSI escape sequences are removed while normal printable
// text (including tab) is preserved.
func TestSanitizeForLog_StripsControlChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"newline stripped", "evil\nlog", "evillog"},
		{"carriage return stripped", "evil\rlog", "evillog"},
		{"ESC stripped", "evil\x1b[31mlog", "evil[31mlog"},
		{"NUL stripped", "evil\x00log", "evillog"},
		{"DEL stripped", "evil\x7flog", "evillog"},
		{"C1 control stripped", "evil\u0085log", "evillog"},
		{"tab preserved", "evil\tlog", "evil\tlog"},
		{"combined newline + ANSI", "evil\nlog\x1b[31mforge", "evillog[31mforge"},
		{"no control chars unchanged", "normal-id-42", "normal-id-42"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForLog(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeForLog(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSanitizeForLog_Truncates verifies that an oversized input is capped at
// maxLogIDLen runes.
func TestSanitizeForLog_Truncates(t *testing.T) {
	oversized := strings.Repeat("A", 1000)
	got := sanitizeForLog(oversized)
	if len([]rune(got)) > maxLogIDLen {
		t.Errorf("sanitizeForLog length = %d runes, want <= %d", len([]rune(got)), maxLogIDLen)
	}
	if len([]rune(got)) != maxLogIDLen {
		t.Errorf("sanitizeForLog length = %d runes, want exactly %d (input far exceeds cap)", len([]rune(got)), maxLogIDLen)
	}
}

// TestSanitizeForLog_ShortInputUnchanged verifies that input under the cap is
// returned verbatim (modulo control-char stripping).
func TestSanitizeForLog_ShortInputUnchanged(t *testing.T) {
	got := sanitizeForLog("short-id")
	if got != "short-id" {
		t.Errorf("sanitizeForLog(\"short-id\") = %q, want \"short-id\"", got)
	}
}
