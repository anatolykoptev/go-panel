package resource_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/anatolykoptev/go-panel/resource"
	"github.com/anatolykoptev/go-panel/tenant"
)

// TestPropose_MatchesHTTPSave drives the same inputs through the HTTP save
// route and through Panel.Propose and requires the same verdict and the same
// values at Writer.Save, so the programmatic path cannot validate more loosely
// (or merge presets differently) than the admin form.
func TestPropose_MatchesHTTPSave(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
	}{
		{"valid", map[string]string{"name": "Item", "status": "active", "note": "{}"}},
		{"required missing", map[string]string{"name": "", "status": "active"}},
		{"select out of whitelist", map[string]string{"name": "Item", "status": "bogus"}},
		{"invalid json", map[string]string{"name": "Item", "note": "{not json"}},
		{"preset wins over submitted value", map[string]string{"name": "Item", "person_id": "999"}},
		{"unknown key dropped", map[string]string{"name": "Item", "junk": "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var httpSaved, propSaved map[string]string
			build := func(dst *map[string]string) (*resource.Panel, resource.Resource) {
				p := newWriterPanel()
				r := writerResource(nil, func(_ context.Context, _ tenant.Tenant, _ string, v map[string]string) error {
					*dst = v
					return nil
				})
				r.Writer.PresetValues = func(context.Context, tenant.Tenant) (map[string]string, error) {
					return map[string]string{"person_id": "5"}, nil
				}
				resource.Register(p, r)
				return p, r
			}

			hp, _ := build(&httpSaved)
			cookieVal, _ := loginAndGetCookie(t, hp)
			form := url.Values{}
			for k, v := range tc.values {
				form.Set(k, v)
			}
			httpRejected := postWriterForm(t, hp, cookieVal, form).Code == http.StatusUnprocessableEntity

			pp, pr := build(&propSaved)
			res, err := pp.Propose(context.Background(), pr, tenant.Tenant{}, "", tc.values)
			if err != nil {
				t.Fatalf("Propose: %v", err)
			}

			if httpRejected == res.Accepted() {
				t.Fatalf("verdict differs: HTTP rejected=%v, Propose accepted=%v (errors %v)", httpRejected, res.Accepted(), res.Errors)
			}
			if !reflect.DeepEqual(httpSaved, propSaved) {
				t.Fatalf("Save saw different values:\nHTTP    %v\nPropose %v", httpSaved, propSaved)
			}
		})
	}
}

func TestPropose_ErrorPaths(t *testing.T) {
	ro := newWriterPanel()
	readOnly := testResource
	resource.Register(ro, readOnly)
	if _, err := ro.Propose(context.Background(), readOnly, tenant.Tenant{}, "", nil); err == nil {
		t.Fatal("a resource without a Writer must be an error")
	}

	p := newWriterPanel()
	var afterErr error
	r := writerResource(nil, func(context.Context, tenant.Tenant, string, map[string]string) error {
		return resource.ErrDetailNotFound
	})
	r.Writer.AfterSave = func(_ context.Context, _ string, err error) { afterErr = err }
	resource.Register(p, r)
	_, err := p.Propose(context.Background(), r, tenant.Tenant{}, "9", map[string]string{"name": "x"})
	if !errors.Is(err, resource.ErrDetailNotFound) {
		t.Fatalf("Save's ErrDetailNotFound must pass through, got %v", err)
	}
	if !errors.Is(afterErr, resource.ErrDetailNotFound) {
		t.Fatalf("AfterSave must see the Save error, got %v", afterErr)
	}
}
