package locale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSet(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		s, err := NewSet("en", "en", "es")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.Default != "en" || len(s.Available) != 2 {
			t.Fatalf("got %+v", s)
		}
	})
	t.Run("empty default", func(t *testing.T) {
		if _, err := NewSet("", "en"); err == nil {
			t.Fatal("want error for empty default")
		}
	})
	t.Run("default not in available", func(t *testing.T) {
		if _, err := NewSet("en", "es", "fr"); err == nil {
			t.Fatal("want error: default not in available")
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		if _, err := NewSet("en", "en", "es", "es"); err == nil {
			t.Fatal("want error for duplicate code")
		}
	})
	t.Run("empty code", func(t *testing.T) {
		if _, err := NewSet("en", "en", ""); err == nil {
			t.Fatal("want error for empty code")
		}
	})
}

func mustSet(t *testing.T) Set {
	t.Helper()
	s, err := NewSet("en", "en", "es")
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	return s
}

func TestSetHasResolve(t *testing.T) {
	s := mustSet(t)
	if !s.Has("es") || s.Has("fr") || s.Has("") {
		t.Fatalf("Has wrong: es=%v fr=%v empty=%v", s.Has("es"), s.Has("fr"), s.Has(""))
	}
	if s.Resolve("es") != "es" {
		t.Error("in-set should resolve to itself")
	}
	if s.Resolve("fr") != "en" {
		t.Error("unknown should resolve to default")
	}
	if s.Resolve("") != "en" {
		t.Error("empty should resolve to default")
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if From(ctx) != "" {
		t.Error("unset context must return empty Locale (default/untranslated)")
	}
	ctx = WithLocale(ctx, "es")
	if From(ctx) != "es" {
		t.Errorf("want es, got %q", From(ctx))
	}
}

func TestQueryResolver(t *testing.T) {
	s := mustSet(t)
	qr := QueryResolver{Set: s} // default param "locale"

	req := func(q string) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/x"+q, nil)
	}

	if got := qr.Resolve(req("?locale=es")); got != "es" {
		t.Errorf("valid locale: want es, got %q", got)
	}
	if got := qr.Resolve(req("")); got != "en" {
		t.Errorf("absent: want default en, got %q", got)
	}
	if got := qr.Resolve(req("?locale=fr")); got != "en" {
		t.Errorf("unknown: want default en, got %q", got)
	}

	// custom param name
	qr2 := QueryResolver{Param: "lang", Set: s}
	if got := qr2.Resolve(req("?lang=es")); got != "es" {
		t.Errorf("custom param: want es, got %q", got)
	}
	if got := qr2.Resolve(req("?locale=es")); got != "en" {
		t.Errorf("custom param ignores 'locale': want default en, got %q", got)
	}
}

func TestMiddleware(t *testing.T) {
	s := mustSet(t)
	var seen Locale
	h := Middleware(QueryResolver{Set: s}, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = From(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?locale=es", nil))
	if seen != "es" {
		t.Errorf("middleware should inject resolved locale; got %q", seen)
	}
	// unknown -> default in context
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?locale=zz", nil))
	if seen != "en" {
		t.Errorf("unknown locale should resolve to default in context; got %q", seen)
	}
}
