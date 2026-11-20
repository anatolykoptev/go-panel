package resource_test

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/components"
	"github.com/anatolykoptev/go-panel/resource"
)

func TestBadgeCell(t *testing.T) {
	tests := []struct {
		name string
		tone components.Tone
		text string
		want string
	}{
		{"escapes text", components.ToneRed, "<script>alert(1)</script>", `<span class="badge badge-red">&lt;script&gt;alert(1)&lt;/script&gt;</span>`},
		{"neutral is the bare chip", components.ToneNeutral, "idle", `<span class="badge">idle</span>`},
		{"unknown tone never becomes a class", components.Tone(99), "x", `<span class="badge">x</span>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resource.BadgeCell(tc.tone, tc.text); got != tc.want {
				t.Fatalf("BadgeCell = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiffCell(t *testing.T) {
	got := resource.DiffCell(`1"><img src=x onerror=alert(1)>`, "<b>2</b>")
	if strings.Contains(got, "<img") || strings.Contains(got, "<b>") {
		t.Fatalf("DiffCell leaked markup: %s", got)
	}
	if !strings.Contains(got, "&rarr;") && !strings.Contains(got, "→") {
		t.Errorf("DiffCell has no arrow in %s", got)
	}
	for _, want := range []string{"<del>1&#34;&gt;&lt;img src=x onerror=alert(1)&gt;</del>", "<ins ", "&lt;b&gt;2&lt;/b&gt;</ins>"} {
		if !strings.Contains(got, want) {
			t.Errorf("DiffCell missing %q in %s", want, got)
		}
	}
	if got := resource.DiffCell("", ""); got != "" {
		t.Errorf("DiffCell(\"\", \"\") = %q, want empty", got)
	}
	if got := resource.DiffCell("", "new"); strings.Contains(got, "<del>") || !strings.Contains(got, "new") {
		t.Errorf("DiffCell with no old value should show only the new one: %s", got)
	}
}
