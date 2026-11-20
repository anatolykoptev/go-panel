package resource

import (
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/components"
)

func flashTestPanel() *Panel {
	return &Panel{csrfKey: []byte("test-csrf-key-32-bytes-long-here")}
}

func TestFlashCodec(t *testing.T) {
	p := flashTestPanel()
	long := strings.Repeat("я", 5000)
	tests := []struct {
		name    string
		in      []components.Notice
		exp     time.Time
		wantN   int
		wantNil bool
	}{
		{"round trip", []components.Notice{{Level: components.NoticeWarn, Title: "T", Text: "x", LinkHref: "/a", LinkText: "go"}}, time.Now().Add(time.Minute), 1, false},
		{"expired", []components.Notice{{Text: "x"}}, time.Now().Add(-time.Second), 0, true},
		{"item cap keeps newest", []components.Notice{{Text: "1"}, {Text: "2"}, {Text: "3"}, {Text: "4"}, {Text: "5"}}, time.Now().Add(time.Minute), 4, false},
		{"oversized text is clipped to fit", []components.Notice{{Text: long}}, time.Now().Add(time.Minute), 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := p.encodeFlash(tc.in, tc.exp)
			if !ok {
				t.Fatal("encode failed")
			}
			if len(v) > flashMaxCookie {
				t.Fatalf("cookie value %d bytes exceeds cap %d", len(v), flashMaxCookie)
			}
			got := p.decodeFlash(v)
			if tc.wantNil != (got == nil) || len(got) != tc.wantN {
				t.Fatalf("decoded %d notices (nil=%v), want %d (nil=%v)", len(got), got == nil, tc.wantN, tc.wantNil)
			}
			if tc.name == "round trip" && (got[0].Level != components.NoticeWarn || got[0].Title != "T" || got[0].LinkText != "go") {
				t.Fatalf("round trip lost fields: %+v", got[0])
			}
			if tc.name == "item cap keeps newest" && got[0].Text != "2" {
				t.Fatalf("cap dropped the wrong end: first = %q, want 2", got[0].Text)
			}
		})
	}
}

func TestFlashCodec_RejectsForgeries(t *testing.T) {
	p := flashTestPanel()
	v, _ := p.encodeFlash([]components.Notice{{Text: "x"}}, time.Now().Add(time.Minute))
	other := &Panel{csrfKey: []byte("another-csrf-key-32-bytes-long!!")}
	for name, in := range map[string]string{
		"other key": func() string {
			o, _ := other.encodeFlash([]components.Notice{{Text: "x"}}, time.Now().Add(time.Minute))
			return o
		}(),
		"no dot": strings.ReplaceAll(v, ".", ""),
		"empty":  "",
		"junk":   "!!!.???",
		"huge":   strings.Repeat("a", flashMaxCookie*3),
	} {
		if got := p.decodeFlash(in); got != nil {
			t.Errorf("%s: accepted a forged flash cookie: %+v", name, got)
		}
	}
}
