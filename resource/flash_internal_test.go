package resource

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
			v, ok := p.encodeFlash(tc.in, tc.exp, "sess")
			if !ok {
				t.Fatal("encode failed")
			}
			if len(v) > flashMaxCookie {
				t.Fatalf("cookie value %d bytes exceeds cap %d", len(v), flashMaxCookie)
			}
			got := p.decodeFlash(v, "sess")
			if tc.wantNil != (got == nil) || len(got) != tc.wantN {
				t.Fatalf("decoded %d notices (nil=%v), want %d (nil=%v)", len(got), got == nil, tc.wantN, tc.wantNil)
			}
			if tc.name == "round trip" && (got[0].Level != components.NoticeWarn || got[0].Title != "T" || got[0].LinkText != "go" || got[0].LinkHref != "/a") {
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
	exp := time.Now().Add(time.Minute)
	v, _ := p.encodeFlash([]components.Notice{{Text: "x"}}, exp, "sess")
	other := &Panel{csrfKey: []byte("another-csrf-key-32-bytes-long!!")}
	o, _ := other.encodeFlash([]components.Notice{{Text: "x"}}, exp, "sess")
	for name, in := range map[string]string{
		"other key": o,
		"no dot":    strings.ReplaceAll(v, ".", ""),
		"empty":     "",
		"junk":      "!!!.???",
		"huge":      strings.Repeat("a", flashMaxCookie*3),
	} {
		if got := p.decodeFlash(in, "sess"); got != nil {
			t.Errorf("%s: accepted a forged flash cookie: %+v", name, got)
		}
	}
}

func TestFlashCodec_BoundToSession(t *testing.T) {
	p := flashTestPanel()
	v, _ := p.encodeFlash([]components.Notice{{Text: "for A"}}, time.Now().Add(time.Minute), "session-A")
	if got := p.decodeFlash(v, "session-A"); len(got) != 1 {
		t.Fatalf("own session rejected: %+v", got)
	}
	if got := p.decodeFlash(v, "session-B"); got != nil {
		t.Fatalf("a cookie minted for session A rendered under session B: %+v", got)
	}
	if got := p.decodeFlash(v, ""); got != nil {
		t.Fatalf("a cookie minted for session A rendered with no session: %+v", got)
	}
}

func TestFlashCodec_ShrinksTextBeforeDroppingNotices(t *testing.T) {
	p := flashTestPanel()
	long := strings.Repeat("я", 600)
	in := []components.Notice{{Text: long}, {Text: long}, {Text: long}, {Text: long}}
	v, ok := p.encodeFlash(in, time.Now().Add(time.Minute), "s")
	if !ok {
		t.Fatal("encode failed")
	}
	got := p.decodeFlash(v, "s")
	if len(got) != 4 {
		t.Fatalf("kept %d of 4 notices; clipping text must come before dropping notices", len(got))
	}
}

func TestFlashCodec_LinkIsWholeOrDropped(t *testing.T) {
	p := flashTestPanel()
	href := "/admin/" + strings.Repeat("a", 500)
	v, _ := p.encodeFlash([]components.Notice{{Text: "x", LinkHref: href, LinkText: "go"}}, time.Now().Add(time.Minute), "s")
	got := p.decodeFlash(v, "s")
	if len(got) != 1 || got[0].LinkHref != "" || got[0].LinkText != "" {
		t.Fatalf("an over-long href must drop the link, not clip it: %+v", got)
	}
	short := "/admin/ok"
	v, _ = p.encodeFlash([]components.Notice{{Text: "x", LinkHref: short, LinkText: "go"}}, time.Now().Add(time.Minute), "s")
	if got := p.decodeFlash(v, "s"); got[0].LinkHref != short {
		t.Fatalf("a short href was altered: %q", got[0].LinkHref)
	}
}

func TestPanelFlash_SecondCallAccumulates(t *testing.T) {
	p := flashTestPanel()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/x", nil)
	w := httptest.NewRecorder()
	p.Flash(w, r, components.Notice{Text: "first"})
	p.Flash(w, r, components.Notice{Text: "second"})
	lines := 0
	for _, l := range w.Header().Values("Set-Cookie") {
		if strings.HasPrefix(l, flashCookie+"=") {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("response carries %d flash Set-Cookie headers, want 1", lines)
	}
	var c *http.Cookie
	for _, x := range w.Result().Cookies() {
		if x.Name == flashCookie {
			c = x
		}
	}
	got := p.decodeFlash(c.Value, "")
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("second Panel.Flash overwrote the first: %+v", got)
	}
}

func TestAddFlash_AfterResponseStartedIsDropped(t *testing.T) {
	p := &Panel{csrfKey: []byte("test-csrf-key-32-bytes-long-here")}
	var ctx context.Context
	h := p.withFlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
		_, _ = w.Write([]byte("body"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil))
	AddFlash(ctx, components.Notice{Text: "late"})
	st := ctx.Value(flashCtxKey{}).(*flashState)
	if len(st.added) != 0 {
		t.Fatalf("a notice added after the response started was queued: %+v", st.added)
	}
}

// capRecorder is a ResponseWriter with the optional interfaces a real net/http
// writer has.
type capRecorder struct{ *httptest.ResponseRecorder }

func (capRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
func (c capRecorder) ReadFrom(r io.Reader) (int64, error)        { return io.Copy(c.ResponseRecorder, r) }

func TestWithFlash_KeepsOptionalWriterInterfaces(t *testing.T) {
	p := flashTestPanel()
	var hijacker, readerFrom bool
	h := p.withFlash(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, hijacker = w.(http.Hijacker)
		_, readerFrom = w.(io.ReaderFrom)
	}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)

	h.ServeHTTP(capRecorder{httptest.NewRecorder()}, req)
	if !hijacker || !readerFrom {
		t.Fatalf("withFlash hid optional interfaces from a writer that has them: Hijacker=%v ReaderFrom=%v", hijacker, readerFrom)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	if hijacker || readerFrom {
		t.Fatalf("withFlash invented interfaces the writer lacks: Hijacker=%v ReaderFrom=%v", hijacker, readerFrom)
	}
}
