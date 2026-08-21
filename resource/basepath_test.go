package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
)

// A Detailer must be able to read the panel's mount prefix.
//
// CrossLinkCell and FilterLinkCell both take a basePath and their docs say to
// pass the panel's "rather than a hardcoded /admin so the link stays correct
// under custom mounts" — but until BasePathFrom there was no way to get it
// inside a Lister/Detailer/Writer, so consumers hardcoded "/admin". go-grad's
// client card did exactly that, which is the bug CrossLinkCell was promoted
// here to fix.
//
// The panel is mounted at a NON-default prefix on purpose: a "/admin" default
// would let a hardcoded constant pass this test.
//
// RED-on-revert: drop withBasePath from Panel.Handler, or pass a literal
// "/admin" to it instead of p.basePath.
func TestBasePathFrom_ReachesADetailer(t *testing.T) {
	const mount = "/operator-panel"

	var seen string
	r := detailerResource("things", func(ctx context.Context, _ *http.Request, _ string) ([]resource.DetailSection, error) {
		seen = resource.BasePathFrom(ctx)
		return []resource.DetailSection{{Title: "T", Items: []resource.DetailItem{{Label: "L", Value: "V"}}}}, nil
	})

	a := newTestAuth()
	p := resource.New(resource.Config{Title: "T", BasePath: mount, Auth: a})
	resource.Register(p, r)

	cookie := authCookie(t, a, "admin", "secret")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, mount+"/things/1", nil)
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookie})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("detail page: %d\n%s", w.Code, w.Body.String())
	}
	if seen != mount {
		t.Errorf("BasePathFrom in a Detailer: got %q, want %q — a consumer building a "+
			"cross-link has to hardcode the prefix without this", seen, mount)
	}
}

// Outside a panel request the answer is empty, not a guess.
func TestBasePathFrom_EmptyOutsideAPanelRequest(t *testing.T) {
	if got := resource.BasePathFrom(context.Background()); got != "" {
		t.Errorf("BasePathFrom(background) = %q, want empty", got)
	}
}
