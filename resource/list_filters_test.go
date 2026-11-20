package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/auth"
	"github.com/anatolykoptev/go-panel/resource"
)

// filtersCapture serves a resource whose Lister records the ListQuery it got.
func filtersCapture(t *testing.T, rawQuery string) map[string][]string {
	t.Helper()
	var got map[string][]string
	called := false
	a := auth.NewHMACAuth(auth.HMACConfig{Username: "op", Password: "pw",
		HMACKey: []byte("0123456789abcdef0123456789abcdef"), BasePath: "/admin"})
	p := resource.New(resource.Config{Title: "t", BasePath: "/admin", Auth: a})
	resource.Register(p, resource.Resource{
		Name: "events", Title: "Events", Icon: "*",
		Sort: admintable.Spec{
			Columns:    []admintable.Column{{Key: "id", Label: "ID", SQLExpr: "id", Sortable: true}},
			DefaultKey: "id", DefaultDir: admintable.Asc,
		},
		Filter: admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "kind", SQLExpr: "kind", Match: admintable.Eq, Allowed: []string{"buy", "sell"}},
			{Key: "tag", SQLExpr: "tag", Match: admintable.AnyOf, Allowed: []string{"a", "b"}},
			{Key: "setting", SQLExpr: "setting", Match: admintable.Eq},
			{Key: "q", SQLExprs: []string{"name"}, Match: admintable.ILike},
		}},
		Lister: func(_ context.Context, q resource.ListQuery) ([]resource.Row, int, error) {
			called = true
			got = q.Filters
			return nil, 0, nil
		},
	})
	lreq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/login",
		strings.NewReader("username=op&password=pw"))
	lreq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lrec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(lrec, lreq)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/events?"+rawQuery, nil)
	for _, c := range lrec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("list not served: status %d, lister called %v", rec.Code, called)
	}
	return got
}

func TestListQuery_Filters(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
		want  map[string][]string
	}{
		{"no filter active", url.Values{}, nil},
		{"eq value in allowed set", url.Values{"kind": {"buy"}}, map[string][]string{"kind": {"buy"}}},
		{"eq value outside allowed set", url.Values{"kind": {"hold"}}, nil},
		{"eq open set passes any value", url.Values{"setting": {"copy.share"}}, map[string][]string{"setting": {"copy.share"}}},
		{"anyof keeps only allowed", url.Values{"tag": {"a", "zzz", "b"}}, map[string][]string{"tag": {"a", "b"}}},
		{"anyof all outside dropped", url.Values{"tag": {"zzz"}}, nil},
		{"ilike free text", url.Values{"q": {"foo"}}, map[string][]string{"q": {"foo"}}},
		{"empty value is absent", url.Values{"kind": {""}, "setting": {""}}, nil},
		{"undeclared key never reaches Filters", url.Values{"secret": {"x"}, "kind": {"sell"}}, map[string][]string{"kind": {"sell"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filtersCapture(t, tc.query.Encode())
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ListQuery.Filters = %#v, want %#v", got, tc.want)
			}
		})
	}
}
