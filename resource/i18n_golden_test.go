package resource_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const i18nGoldenDir = "testdata/i18n"

// TestDefaultsRenderUnchanged pins the contract every existing consumer relies
// on: a panel that sets no Strings renders EXACTLY what it rendered before the
// strings were configurable — including the Russian "Вернуть" the Trash and
// undo toast always carried.
//
// The golden files were captured from v0.23.16, before any template changed.
// Regenerate deliberately with UPDATE_GOLDEN=1 and read the diff: a changed
// golden is a changed page for every consumer.
func TestDefaultsRenderUnchanged(t *testing.T) {
	got := i18nPagesDump(pagesFor(t, i18nOpts{}))
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(i18nGoldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range got {
			if err := os.WriteFile(filepath.Join(i18nGoldenDir, name+".golden"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(i18nGoldenDir, name+".golden"))
		if err != nil {
			t.Errorf("%s: no golden file (%v)", name, err)
			continue
		}
		if got[name] != string(want) {
			t.Errorf("%s: default render changed (first diff at byte %d)\n%s", name, firstDiff(got[name], string(want)), diffWindow(got[name], string(want)))
		}
	}
}

func firstDiff(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func diffWindow(got, want string) string {
	i := firstDiff(got, want)
	lo := max(0, i-80)
	return "got:  " + got[lo:min(len(got), i+120)] + "\nwant: " + want[lo:min(len(want), i+120)]
}
