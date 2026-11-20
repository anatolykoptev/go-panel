package resource

import (
	"net/url"

	"github.com/anatolykoptev/go-panel/tenant"
)

// ListWhere composes the WHERE fragment and bind args a Lister receives for r:
// the resource's declared Filter applied to the submitted values q, then the
// tenant scope clause for t appended AFTER the filter args so the positional
// placeholders stay correct.
//
// It is the one composition behind both the HTTP list page and the MCP list
// tool, so the two entry points cannot drift on tenant scoping: a resource that
// declares Scope is constrained to t's rows whichever way it is listed. A
// resource with an empty Scope is global and gets no scope clause.
//
// q may be nil. Keys q carries that r.Filter does not declare are ignored, and
// a value outside a filter's Allowed set degrades to "no filter" (admintable's
// safe-degrade convention); only the scope clause is unconditional.
func (r Resource) ListWhere(q url.Values, t tenant.Tenant) (conds string, args []any) {
	whereConds, whereArgs := r.Filter.Where(q, 1)

	var tenantCond string
	if r.Scope.Column != "" {
		tc, ta := tenant.ScopeClause(r.Scope, t, len(whereArgs)+1)
		if tc != "" {
			tenantCond = tc
			whereArgs = append(whereArgs, ta)
		}
	}
	return combineConds(whereConds, tenantCond), whereArgs
}
