package resource_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// TestSaveError_ErrorMessage verifies SaveError.Error() returns Message.
func TestSaveError_ErrorMessage(t *testing.T) {
	se := resource.NewSaveError("name", "эти даты уже заняты")
	if se.Field != "name" {
		t.Errorf("Field = %q, want %q", se.Field, "name")
	}
	if se.Error() != "эти даты уже заняты" {
		t.Errorf("Error() = %q, want %q", se.Error(), "эти даты уже заняты")
	}
}

// TestSaveError_ErrorsAsThroughWrap verifies errors.As unwraps SaveError
// through a fmt.Errorf("...: %w", err) wrap — saveHandler relies on this to
// detect a SaveError returned by a Writer.Save that adds its own context.
func TestSaveError_ErrorsAsThroughWrap(t *testing.T) {
	inner := resource.NewSaveError("name", "эти даты уже заняты")
	wrapped := fmt.Errorf("zonestore: save: %w", inner)

	var se *resource.SaveError
	if !errors.As(wrapped, &se) {
		t.Fatal("errors.As failed to unwrap a wrapped SaveError")
	}
	if se != inner {
		t.Errorf("errors.As target = %v, want %v", se, inner)
	}
}

// postSaveHandlerTest issues an authenticated, CSRF-valid POST to a save path
// against a writerResource-shaped panel, mirroring writer_test.go's request
// construction (postSave in locale_form_test.go targets a different resource
// shape, so this stays local to keep the fixtures decoupled).
func postSaveHandlerTest(t *testing.T, p *resource.Panel, cookieVal string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form.Set("_csrf", tok)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/items/new/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	return w
}

// TestSaveHandler_SaveErrorRenders422WithFieldMessage verifies that a
// Writer.Save returning a *resource.SaveError renders the form again at 422
// with the message shown, instead of an opaque 500 — the domain error (e.g.
// go-grad's ErrSlotOccupied) reaches the operator as an actionable message.
// The row must not be treated as persisted (no redirect/HX-Redirect).
func TestSaveHandler_SaveErrorRenders422WithFieldMessage(t *testing.T) {
	saveCount := 0
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			saveCount++
			return resource.NewSaveError("name", "эти даты уже заняты")
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	form := url.Values{"name": {"My Item"}}
	w := postSaveHandlerTest(t, p, cookieVal, form)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if saveCount != 1 {
		t.Errorf("expected Save to be called once, called %d times", saveCount)
	}
	body := w.Body.String()
	if !strings.Contains(body, "эти даты уже заняты") {
		t.Errorf("expected the SaveError message in the re-rendered form body, got: %s", body)
	}
	if strings.Contains(body, "save failed") {
		t.Errorf("must not fall back to the opaque 'save failed' body when a SaveError is returned")
	}
	if w.Header().Get("HX-Redirect") != "" {
		t.Errorf("must not redirect (row not persisted) when Save returns a SaveError")
	}
}

// TestSaveHandler_WrappedSaveErrorRenders422 verifies errors.As detection
// works through a wrapped SaveError, as a real Writer.Save is likely to add
// its own context (e.g. fmt.Errorf("zonestore: %w", resource.NewSaveError(...))).
func TestSaveHandler_WrappedSaveErrorRenders422(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return fmt.Errorf("zonestore: save: %w", resource.NewSaveError("name", "these dates are already booked"))
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	form := url.Values{"name": {"My Item"}}
	w := postSaveHandlerTest(t, p, cookieVal, form)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a wrapped SaveError, got %d\nbody: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "these dates are already booked") {
		t.Errorf("expected the wrapped SaveError message in the re-rendered form body")
	}
}

// TestSaveHandler_PlainErrorStill500 verifies a genuine internal error (not a
// SaveError) is NOT masked as a user error — it must still 500 with the
// generic body and must not re-render the form.
func TestSaveHandler_PlainErrorStill500(t *testing.T) {
	p := newWriterPanel()
	resource.Register(p, writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return errors.New("boom")
		},
	))
	cookieVal, _ := loginAndGetCookie(t, p)

	form := url.Values{"name": {"My Item"}}
	w := postSaveHandlerTest(t, p, cookieVal, form)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a plain error, got %d\nbody: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "save failed") {
		t.Errorf("expected the generic 'save failed' body, got: %s", body)
	}
	if strings.Contains(body, `name="name"`) {
		t.Errorf("form must NOT be re-rendered on a plain internal error, got: %s", body)
	}
}
