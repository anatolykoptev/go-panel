package resource

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// failingResponseWriter is an http.ResponseWriter whose Write always fails,
// forcing the templ.Component.Render call inside renderListFragment to
// return a non-nil error — otherwise unreachable from an httptest-level test
// (a real templ render over valid listPageData never errors on its own).
type failingResponseWriter struct {
	header     http.Header
	statusCode int
	body       strings.Builder
	writeErr   error
}

func (f *failingResponseWriter) Header() http.Header { return f.header }

func (f *failingResponseWriter) Write(b []byte) (int, error) {
	// Record what was attempted (for leak assertions) but report failure, as
	// a broken client connection / io error downstream of the buffer would.
	f.body.Write(b) //nolint:errcheck // test double; capturing best-effort for assertions
	return 0, f.writeErr
}

func (f *failingResponseWriter) WriteHeader(code int) { f.statusCode = code }

func newFailingResponseWriter() *failingResponseWriter {
	return &failingResponseWriter{header: make(http.Header), writeErr: errors.New("write: broken pipe")}
}

// TestRenderListFragment_RenderErrorDoesNotLeakDetails verifies that when the
// underlying Render call fails (e.g. a broken client connection), the
// response carries a generic 500 body — the render error's text must never
// reach the client. This is the sibling fix to the Lister/layoutComp.Render
// error-hygiene fix: renderListFragment (the HTMX list + Load-more path) had
// the same http.Error(w, err.Error(), 500) leak and no server-side log.
func TestRenderListFragment_RenderErrorDoesNotLeakDetails(t *testing.T) {
	data := listPageData{
		Resource: Resource{Name: "widgets"},
		Rows: []Row{
			{ID: "1", Cells: []Cell{{Value: "Widget A"}}},
		},
		BasePath: "/admin",
	}

	fw := newFailingResponseWriter()
	renderListFragment(context.Background(), fw, data, false)

	if fw.statusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", fw.statusCode)
	}
	if strings.Contains(fw.body.String(), "broken pipe") {
		t.Errorf("response body leaked the raw render error: %q", fw.body.String())
	}
}
