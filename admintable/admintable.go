// Package admintable provides a SQL-injection-safe sortable and filterable
// table abstraction for server-rendered admin pages.
//
// Security model: only Spec-owned SQLExpr values + the literal strings
// "ASC", "DESC", and (when NullsLast is set) " NULLS LAST" ever reach an
// ORDER BY clause. URL sort and dir parameters are equality-matched against
// a closed set of author-declared keys — they are never interpolated into SQL.
//
// FilterSpec works by the same principle: only the author-declared SQLExpr
// values and literal comparison operators reach the WHERE clause; URL values
// are bound as positional parameters (bind args), never interpolated.
//
// Call Spec.Valid() and FilterSpec.Valid() at program startup to catch
// misconfiguration early.
package admintable

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// --- Sort ---

// Column is a vetted, declarative column definition for ORDER BY.
// SQLExpr is the ONLY value ever concatenated into ORDER BY — author-supplied,
// never from the URL.
type Column struct {
	Key             string // stable URL token, e.g. "name", "updated"
	Label           string // header display text
	Sortable        bool
	SQLExpr         string // vetted ORDER BY expression, e.g. "p.name". Compile-time constant.
	NullsLast       bool   // emit "<expr> <DIR> NULLS LAST"
	TieBreakSQLExpr string // optional secondary sort term, author-constant
	Width           string // optional inline width, e.g. "24%"
	Align           string // "", "center", "right"
}

// Dir is the sort direction.
type Dir string

const (
	Asc  Dir = "asc"
	Desc Dir = "desc"
)

// Spec is one table's sort contract.
type Spec struct {
	Columns    []Column
	DefaultKey string
	DefaultDir Dir
}

// State is the resolved, safe sort selection.
type State struct {
	Key string
	Dir Dir
}

// Valid reports whether the Spec is correctly configured.
func (sp Spec) Valid() error {
	seen := make(map[string]bool, len(sp.Columns))
	for _, col := range sp.Columns {
		if seen[col.Key] {
			return fmt.Errorf("admintable.Spec: duplicate column Key %q", col.Key)
		}
		seen[col.Key] = true
	}
	hasSortable := false
	defaultKeyIsSortable := false
	for _, col := range sp.Columns {
		if col.Sortable {
			hasSortable = true
			if col.Key == sp.DefaultKey {
				defaultKeyIsSortable = true
			}
		}
	}
	if !hasSortable {
		return errors.New("admintable.Spec: no Sortable columns defined")
	}
	if !defaultKeyIsSortable {
		return fmt.Errorf("admintable.Spec: DefaultKey %q does not name a Sortable column", sp.DefaultKey)
	}
	return nil
}

// Resolve returns the safe, validated sort State for the given URL parameters.
// Raw URL bytes are never stored; they are only equality-matched against the
// closed set of Spec-declared keys.
func (sp Spec) Resolve(sortKey, dir string) State {
	defaultDir := sp.DefaultDir
	if defaultDir != Asc && defaultDir != Desc {
		defaultDir = Asc
	}
	st := State{Key: sp.DefaultKey, Dir: defaultDir}
	for _, col := range sp.Columns {
		if col.Sortable && col.Key == sortKey {
			st.Key = col.Key
			break
		}
	}
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case "asc":
		st.Dir = Asc
	case "desc":
		st.Dir = Desc
	}
	return st
}

// OrderBy returns the ORDER BY fragment for st. Built entirely from
// author-declared SQLExpr + literal direction/NULLS constants.
//
// Callers passing this output to fmt.Sprintf should annotate:
//
//	//nolint:gosec // only Spec-owned SQLExpr + literal "ASC"/"DESC" +
//	// optional " NULLS LAST" reach SQL; URL params equality-matched, never interpolated.
func (sp Spec) OrderBy(st State) string {
	expr := sp.defaultExpr()
	nullsLast := false
	var tieBreak string
	for _, col := range sp.Columns {
		if col.Sortable && col.Key == st.Key {
			expr = col.SQLExpr
			nullsLast = col.NullsLast
			tieBreak = col.TieBreakSQLExpr
			break
		}
	}
	dir := " ASC"
	if st.Dir == Desc {
		dir = " DESC"
	}
	result := expr + dir
	if nullsLast {
		result = expr + dir + " NULLS LAST"
	}
	if tieBreak != "" {
		//nolint:gosec // only author-declared compile-time TieBreakSQLExpr constant; never URL input.
		result += ", " + tieBreak
	}
	return result
}

func (sp Spec) defaultExpr() string {
	var firstSortable string
	for _, col := range sp.Columns {
		if col.Sortable {
			if firstSortable == "" {
				firstSortable = col.SQLExpr
			}
			if col.Key == sp.DefaultKey {
				return col.SQLExpr
			}
		}
	}
	if firstSortable != "" {
		return firstSortable
	}
	return "1"
}

// --- Filter ---

// MatchKind is the comparison operator for a filter.
type MatchKind int

const (
	// Eq generates: SQLExpr = $N
	Eq MatchKind = iota
	// AnyOf generates: SQLExpr = ANY($N) when multiple values are provided,
	// or SQLExpr = $N for a single value.
	AnyOf
	// ILike generates: SQLExpr ILIKE $N (pattern is %<value>%).
	ILike
)

// Filter is one filterable dimension declared by a Resource author.
// Only SQLExpr (author-constant) reaches SQL; URL values become bind args.
type Filter struct {
	Key     string    // URL query param name, e.g. "status", "q"
	SQLExpr string    // table-qualified column, e.g. "p.status". Compile-time constant.
	Match   MatchKind // comparison operator
	// Allowed is the closed set of accepted values for Eq/AnyOf filters.
	// Empty means any non-empty value is accepted (used for ILike).
	Allowed []string
}

// FilterSpec declares all filterable dimensions for a Resource.
type FilterSpec struct {
	Filters []Filter
}

// Valid reports whether the FilterSpec is correctly configured.
func (fs FilterSpec) Valid() error {
	seen := make(map[string]bool, len(fs.Filters))
	for _, f := range fs.Filters {
		if seen[f.Key] {
			return fmt.Errorf("admintable.FilterSpec: duplicate filter Key %q", f.Key)
		}
		seen[f.Key] = true
		if f.SQLExpr == "" {
			return fmt.Errorf("admintable.FilterSpec: filter %q has empty SQLExpr", f.Key)
		}
	}
	return nil
}

// Where builds a WHERE clause fragment and bind args from query params.
// Only author-declared SQLExpr values + literal comparison operators appear
// in the fragment; URL values are returned as bind args (positional).
//
// The returned conds string is empty when no active filters exist.
// The caller is responsible for prepending "WHERE " or "AND " as appropriate.
//
// startArg is the first positional arg index (1-based for pgx). Pass 1 unless
// your query already has bound args before the filters.
//
// Callers should annotate fmt.Sprintf usage of conds:
//
//	//nolint:gosec // only FilterSpec-owned SQLExpr + literal operators reach SQL.
func (fs FilterSpec) Where(params url.Values, startArg int) (conds string, args []any) {
	var parts []string
	argIdx := startArg
	for _, f := range fs.Filters {
		val := strings.TrimSpace(params.Get(f.Key))
		if val == "" {
			continue
		}
		// For Eq/AnyOf: reject values not in the allowed set.
		if len(f.Allowed) > 0 && f.Match != ILike {
			allowed := false
			for _, a := range f.Allowed {
				if a == val {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		switch f.Match {
		case Eq:
			//nolint:gosec // only author-declared SQLExpr + literal "= $N"; val is a bind arg.
			parts = append(parts, fmt.Sprintf("%s = $%d", f.SQLExpr, argIdx))
			args = append(args, val)
			argIdx++
		case AnyOf:
			//nolint:gosec // only author-declared SQLExpr + literal "= $N"; val is a bind arg.
			parts = append(parts, fmt.Sprintf("%s = $%d", f.SQLExpr, argIdx))
			args = append(args, val)
			argIdx++
		case ILike:
			//nolint:gosec // only author-declared SQLExpr + literal "ILIKE $N"; pattern built from bind arg only.
			parts = append(parts, fmt.Sprintf("%s ILIKE $%d", f.SQLExpr, argIdx))
			args = append(args, "%"+val+"%")
			argIdx++
		}
	}
	return strings.Join(parts, " AND "), args
}
