package resource

import (
	"net/url"

	"github.com/anatolykoptev/go-kit/admintable"
)

// structuredFilters resolves the URL values of the declared filters into the
// data a non-SQL Lister reads as [ListQuery.Filters].
//
// It applies the same admission rules as [admintable.FilterSpec.Where]: only
// declared keys are read, a closed set (Filter.Allowed) drops values outside
// it, and an empty value is treated as absent. A key with no admitted value is
// omitted from the map, so Filters is nil when no filter is active.
func structuredFilters(spec admintable.FilterSpec, q url.Values) map[string][]string {
	var out map[string][]string
	add := func(key string, vals []string) {
		if len(vals) == 0 {
			return
		}
		if out == nil {
			out = make(map[string][]string, len(spec.Filters))
		}
		out[key] = vals
	}
	for _, f := range spec.Filters {
		switch f.Match {
		case admintable.AnyOf:
			var kept []string
			for _, v := range q[f.Key] {
				if v != "" && filterAllows(f, v) {
					kept = append(kept, v)
				}
			}
			add(f.Key, kept)
		default: // Eq, ILike: one value
			if v := q.Get(f.Key); v != "" && filterAllows(f, v) {
				add(f.Key, []string{v})
			}
		}
	}
	return out
}

func filterAllows(f admintable.Filter, v string) bool {
	if len(f.Allowed) == 0 {
		return true
	}
	for _, a := range f.Allowed {
		if a == v {
			return true
		}
	}
	return false
}
