package resource_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/csrf"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// doDeleteRequestErr sends an authenticated POST /{id}/delete with a CSRF
// token. The Writer.Delete closure returns an error to trigger the error log
// path where the id is emitted as a structured log attribute. Returns the
// captured slog text output.
func doDeleteRequestErr(t *testing.T, p *resource.Panel, idPath string) (logOut string, statusCode int) {
	t.Helper()
	cookieVal, _ := loginAndGetCookie(t, p)
	tok := csrf.Issue(testCSRFKey, cookieVal, csrf.DefaultTTL)
	form := url.Values{"_csrf": {tok}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/admin/items/"+idPath+"/delete",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "panel_admin", Value: cookieVal})

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, req)
	slog.SetDefault(prev)

	return buf.String(), w.Code
}

// TestDeleteHandler_LogSanitisation_NoControlChars (F1) verifies that an id
// containing a newline and an ANSI escape sequence is sanitised before
// reaching the structured log. The sanitised id attribute must not contain
// the escaped control-character sequences that the TextHandler produces when
// fed raw control chars.
//
// Mutation: at the call site (resource.go deleteHandler slog.Error), pass the
// raw id instead of sanitizeForLog(id). F1 → RED: the log would contain
// id="evil\nlog\x1b[31mforge" (quoted, with \n and \x1b escape sequences).
func TestDeleteHandler_LogSanitisation_NoControlChars(t *testing.T) {
	p := newWriterPanel()
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	)
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error {
		return errors.New("forced delete error")
	}
	resource.Register(p, r)

	// Craft an id with a newline (log forgery) and an ANSI red escape (terminal
	// control). Percent-encode them so the mux URL-decodes them into the path
	// value: %0A = \n, %1B = ESC.
	craftedID := "evil%0Alog%1B[31mforge"
	logOut, code := doDeleteRequestErr(t, p, craftedID)

	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from delete error, got %d", code)
	}

	// F1: the id attribute must be the sanitised value (control chars stripped),
	// not the raw value (which the TextHandler would quote and escape).
	// Sanitised: id=evillog[31mforge (unquoted, no escape sequences)
	// Raw (mutation): id="evil\nlog\x1b[31mforge" (quoted, with \n and \x1b)
	if strings.Contains(logOut, `\x1b`) {
		t.Errorf("F1: ESC escape sequence \\x1b found in log — control char not stripped from id\nlog:\n%s", logOut)
	}
	if strings.Contains(logOut, `\n`) {
		// Check it's in the id attribute, not the msg or err fields.
		// The msg is "resource: delete failed" (no \n), err is "forced delete error" (no \n).
		// So any \n in the output must come from the id.
		t.Errorf("F1: newline escape sequence \\n found in log — control char not stripped from id\nlog:\n%s", logOut)
	}
	// Verify the sanitised id value is present (control chars gone, not just escaped).
	if !strings.Contains(logOut, "id=evillog[31mforge") {
		t.Errorf("F1: expected sanitised id=evillog[31mforge in log, not found\nlog:\n%s", logOut)
	}
}

// TestDeleteHandler_LogSanitisation_BoundedLength (F2) verifies that an
// oversized id is truncated to maxLogIDLen (256) runes in the log attribute.
//
// Mutation: raise maxLogIDLen to a value larger than the test input (e.g.
// 10000). F2 → RED: the id attribute would be 1000 runes, far exceeding 256.
func TestDeleteHandler_LogSanitisation_BoundedLength(t *testing.T) {
	p := newWriterPanel()
	r := writerResource(
		func(_ context.Context, _ tenant.Tenant, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		func(_ context.Context, _ tenant.Tenant, _ string, _ map[string]string) error {
			return nil
		},
	)
	r.Writer.Delete = func(_ context.Context, _ tenant.Tenant, _ string) error {
		return errors.New("forced delete error")
	}
	resource.Register(p, r)

	// An id far over the 256-rune cap: 1000 'A' characters.
	oversizedID := strings.Repeat("A", 1000)
	logOut, code := doDeleteRequestErr(t, p, oversizedID)

	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from delete error, got %d", code)
	}

	// F2: extract the id= value from the log and check its rune length.
	// TextHandler format: ... id=AAAA... err=...
	idStart := strings.Index(logOut, "id=")
	if idStart < 0 {
		t.Fatalf("F2: no id= attribute found in log output\nlog:\n%s", logOut)
	}
	idStart += len("id=")
	idEnd := strings.Index(logOut[idStart:], " err=")
	if idEnd < 0 {
		t.Fatalf("F2: could not find end of id attribute (no ' err=' after id=)\nlog:\n%s", logOut)
	}
	idValue := logOut[idStart : idStart+idEnd]
	if got := len([]rune(idValue)); got > 256 {
		t.Errorf("F2: id attribute length = %d runes, want <= 256\nid value (first 300 runes): %.300q", got, idValue)
	}
}
