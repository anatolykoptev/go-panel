package resource_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/shell"
)

// russianOpts is a consumer that wants its admin fully in Russian.
func russianOpts() i18nOpts {
	return i18nOpts{
		cfg: func(c *resource.Config) { c.Strings = resource.RussianStrings() },
		res: func(r *resource.Resource) {
			r.FilterGroupLabels = map[string]string{"status": "Статус", "q": "Название"}
		},
	}
}

var (
	styleScriptRE = regexp.MustCompile(`(?is)<(style|script|code)\b.*?</(style|script|code)>`)
	tagRE         = regexp.MustCompile(`(?s)<[^>]*>`)
	// Attributes whose values a user reads, hears (screen readers) or is asked
	// to confirm. Everything else (class, id, href, style, value, name…) is
	// markup, not copy.
	visibleAttrRE = regexp.MustCompile(`(?s)\b(aria-label|title|alt|placeholder|data-confirm|data-label[a-z-]*)="([^"]*)"`)
	latinWordRE   = regexp.MustCompile(`[A-Za-z]{2,}`)
	htmlLangRE    = regexp.MustCompile(`<html lang="([^"]*)"`)
)

// visibleText is what a person (or a screen reader) is shown by a page: its
// text nodes plus the attributes that carry copy.
func visibleText(body string) string {
	var attrs []string
	for _, m := range visibleAttrRE.FindAllStringSubmatch(body, -1) {
		attrs = append(attrs, m[2])
	}
	// Recovery codes are random tokens, not copy.
	body = recoveryItemRE.ReplaceAllString(body, " ")
	text := tagRE.ReplaceAllString(styleScriptRE.ReplaceAllString(body, " "), " ")
	return html.UnescapeString(text + " " + strings.Join(attrs, " "))
}

// latinLeft returns the Latin words a page still shows, minus allow.
func latinLeft(body string, allow map[string]bool) []string {
	seen := map[string]bool{}
	for _, w := range latinWordRE.FindAllString(visibleText(body), -1) {
		if !allow[w] {
			seen[w] = true
		}
	}
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// Locale codes are data (the switcher prints them upper-cased), not copy.
//
// "admin" is the fixture account's ROLE, shown under the user name; roles are
// data the consumer defines, not panel copy.
var localeCodes = map[string]bool{"RU": true, "EN": true, "admin": true}

// Names of the authenticator apps the Russian setup text lists, and the format
// name «JSON» in a validation message: proper names and acronyms, not English
// sentences. Listed here rather than skipped by length so a real leftover next
// to them still fails.
var russianAllow = map[string]bool{
	"RU": true, "EN": true, "admin": true, "JSON": true,
	"Google": true, "Authenticator": true, "Password": true, "Authy": true, "QR": true,
}

// TestRussianStrings_RendersNoEnglish is the audit: every audited surface —
// list, form, validation errors, detail, trash, sidebar, login, second factor,
// 2FA enrollment — rendered for a consumer on RussianStrings() with Cyrillic
// fixtures must show no Latin word. A literal added to a template and not
// routed through Strings shows up here as the word itself.
func TestRussianStrings_RendersNoEnglish(t *testing.T) {
	for _, p := range pagesFor(t, russianOpts()) {
		if left := latinLeft(p.body, russianAllow); len(left) > 0 {
			t.Errorf("%s (HTTP %d): English left on a Russian panel: %v", p.name, p.code, left)
		}
		if m := htmlLangRE.FindStringSubmatch(p.body); m != nil && m[1] != "ru" {
			t.Errorf("%s: <html lang=%q>, want \"ru\"", p.name, m[1])
		}
	}
}

// markerStrings builds a Strings whose every field renders as a unique
// Cyrillic marker «Ж<field index>Ж». Cyrillic so the same Latin scan that
// audits the Russian preset applies; unique so a page can be asked which
// fields it actually used.
func markerStrings() (resource.Strings, map[int]string) {
	var s resource.Strings
	v := reflect.ValueOf(&s).Elem()
	markers := map[int]string{}
	for i := range v.NumField() {
		m := fmt.Sprintf("Ж%dЖ", i)
		markers[i] = m
		f := v.Field(i)
		switch f.Kind() { //nolint:exhaustive // Strings holds only string and func fields; TestStringsShape asserts it
		case reflect.String:
			f.SetString(m)
		case reflect.Func:
			f.Set(reflect.MakeFunc(f.Type(), func(args []reflect.Value) []reflect.Value {
				var b strings.Builder
				b.WriteString(m)
				for _, a := range args {
					fmt.Fprint(&b, "·", a.Interface())
				}
				return []reflect.Value{reflect.ValueOf(b.String())}
			}))
		}
	}
	return s, markers
}

// TestStrings_EveryFieldIsConsumed proves no Strings field is dead and no
// audited literal escaped the struct: with every field set to a marker, each
// marker must appear on some page, and no English may remain anywhere.
//
// A field nothing renders is a setting that silently does nothing — the
// consumer sets it, sees no change, and cannot tell why.
func TestStrings_EveryFieldIsConsumed(t *testing.T) {
	s, markers := markerStrings()
	pages := pagesFor(t, i18nOpts{
		cfg: func(c *resource.Config) { c.Strings = s },
		res: func(r *resource.Resource) {
			r.FilterGroupLabels = map[string]string{"status": "Статус", "q": "Название"}
		},
	})
	var all strings.Builder
	for _, p := range pages {
		all.WriteString(p.body)
		all.WriteString("\n")
		if left := latinLeft(p.body, localeCodes); len(left) > 0 {
			t.Errorf("%s (HTTP %d): literal not routed through Strings: %v", p.name, p.code, left)
		}
	}
	typ := reflect.TypeFor[resource.Strings]()
	for i := range typ.NumField() {
		if !strings.Contains(all.String(), markers[i]) {
			t.Errorf("Strings.%s is never rendered by any audited page", typ.Field(i).Name)
		}
	}
}

// A consumer that sets only part of Strings keeps the rest in English: a
// partial override must not blank the fields it did not name.
func TestStrings_PartialOverrideKeepsDefaults(t *testing.T) {
	pages := pagesFor(t, i18nOpts{cfg: func(c *resource.Config) {
		c.Strings = resource.Strings{New: "+ Добавить"}
	}})
	for _, p := range pages {
		if p.name != "list" {
			continue
		}
		for _, want := range []string{"+ Добавить", ">Edit</a>", "120 total"} {
			if !strings.Contains(p.body, want) {
				t.Errorf("list page lacks %q — partial override lost a default or the override", want)
			}
		}
		if strings.Contains(p.body, "+ New") {
			t.Error("the overridden New string still renders its English default")
		}
	}
}

// FilterGroupLabels replaces the raw Filter.Key caption (and the key inside a
// text filter's placeholder); a key that is not a filter is a typo and panics
// at Register, like FilterLabels.
func TestFilterGroupLabels(t *testing.T) {
	render := func(labels map[string]string) string {
		r := statusResource(nil, nil)
		r.FilterGroupLabels = labels
		return renderList(t, r, "")
	}
	if body := render(nil); !strings.Contains(body, ">status</span>") {
		t.Error("without FilterGroupLabels the caption must stay the raw key")
	}
	body := render(map[string]string{"status": "Статус"})
	if !strings.Contains(body, ">Статус</span>") || strings.Contains(body, ">status</span>") {
		t.Errorf("FilterGroupLabels caption not rendered:\n%s", body)
	}

	// Text filter: the placeholder uses the caption, not the key.
	r := statusResource(nil, nil)
	r.Filter.Filters = append(r.Filter.Filters, searchFilter())
	r.FilterGroupLabels = map[string]string{"q": "Название"}
	if body := renderList(t, r, ""); !strings.Contains(body, `placeholder="Search Название..."`) {
		t.Errorf("text filter placeholder ignores FilterGroupLabels:\n%s", body)
	}

	defer func() {
		if recover() == nil {
			t.Error("Register must panic on a FilterGroupLabels key that is not a filter")
		}
	}()
	render(map[string]string{"nosuch": "x"})
}

// ProfileName replaces the raw session user id in the sidebar; an empty answer
// and a panic both fall back to the id; a name set via SetProfile wins and the
// func is then not even called.
func TestProfileName(t *testing.T) {
	type tc struct {
		name       string
		fn         func(string) string
		static     string
		want       string
		wantCalled int
	}
	for _, c := range []tc{
		{"resolved", func(id string) string { return "Анна (" + id + ")" }, "", "Анна (u1)", 1},
		{"empty falls back to id", func(string) string { return "" }, "", "u1", 1},
		{"panic falls back to id", func(string) string { panic("store down") }, "", "u1", 1},
		{"static SetProfile name wins", func(string) string { return "ignored" }, "Статичное", "Статичное", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			store := newTOTPTestStore()
			i18nSeed(t, store.testAccountStore, "u1", "op@example.com", "pw", "админ")
			a := i18nBcryptAuth(store, nil, auth.RateRule{})
			p := resource.New(resource.Config{
				Title: "Панель", BasePath: "/admin", Auth: a, CSRFKey: i18nCSRFKey,
				ProfileName: func(_ context.Context, id string) string { calls++; return c.fn(id) },
			})
			p.SetProfile(shell.ProfileConfig{Name: c.static})
			resource.Register(p, i18nEmptyList())
			ck := bcryptLogin(t, a, "op@example.com", "pw")
			w := httptest.NewRecorder()
			p.Handler().ServeHTTP(w, i18nRequest(http.MethodGet, "/admin/nothing", nil, []*http.Cookie{ck}, false))
			if got := profileNameIn(w.Body.String()); got != c.want {
				t.Errorf("sidebar name = %q, want %q", got, c.want)
			}
			if calls != c.wantCalled {
				t.Errorf("ProfileName called %d times per full-page render, want %d", calls, c.wantCalled)
			}

			// An htmx rows fragment has no sidebar, so it must not pay for the lookup.
			calls = 0
			w = httptest.NewRecorder()
			p.Handler().ServeHTTP(w, i18nRequest(http.MethodGet, "/admin/nothing/rows", nil, []*http.Cookie{ck}, true))
			if calls != 0 {
				t.Errorf("ProfileName called %d times for a rows fragment, want 0", calls)
			}
		})
	}
}

var profileNameRE = regexp.MustCompile(`class="sidebar-profile-name">([^<]*)<`)

func profileNameIn(body string) string {
	m := profileNameRE.FindStringSubmatch(body)
	if m == nil {
		return "<no profile block>"
	}
	return html.UnescapeString(m[1])
}

func searchFilter() admintable.Filter {
	return admintable.Filter{Key: "q", SQLExprs: []string{"name"}, Match: admintable.ILike}
}
