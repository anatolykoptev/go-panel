package resource

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/admintable"
	"github.com/anatolykoptev/go-panel/tenant"
)

// relPanel builds a minimal *Panel (no auth/mux) carrying the given
// resources, with basePath /admin. resolveRelations only reads p.resources
// and p.basePath, so this is sufficient for unit testing.
func relPanel(resources ...Resource) *Panel {
	return &Panel{
		basePath:  "/admin",
		resources: append([]Resource{}, resources...),
	}
}

// postsResource builds a resource named "posts" with an id + author_id
// column and the given Relations.
func postsResource(rels ...Relation) *Resource {
	return &Resource{
		Name: "posts",
		Sort: admintable.Spec{
			Columns: []admintable.Column{
				{Key: "id", Sortable: true, SQLExpr: "p.id"},
				{Key: "author_id", Sortable: true, SQLExpr: "p.author_id"},
			},
		},
		Relations: rels,
	}
}

// usersTarget builds a target "users" resource with the given Scope and
// FetchRow closure.
func usersTarget(scope tenant.Scope, fetchRow func(ctx context.Context, id string) (map[string]string, error)) Resource {
	return Resource{
		Name:     "users",
		Sort:     admintable.Spec{Columns: []admintable.Column{{Key: "id", Sortable: true, SQLExpr: "u.id"}}},
		Scope:    scope,
		FetchRow: fetchRow,
	}
}

// postRow is a tiny helper to build a Row with id + author_id cells.
func postRow(id, authorID string) Row {
	return Row{
		ID: id,
		Cells: []Cell{
			{Value: id},
			{Value: authorID},
		},
	}
}

// TestResolveRelations_NoOpWhenNoRelations: zero Relations -> rows unchanged.
func TestResolveRelations_NoOpWhenNoRelations(t *testing.T) {
	p := relPanel()
	r := postsResource() // no relations
	rows := []Row{postRow("1", "42"), postRow("2", "43")}
	before0 := rows[0].Cells[1].Value
	before0HTML := rows[0].Cells[1].HTML
	before1 := rows[1].Cells[1].Value
	before1HTML := rows[1].Cells[1].HTML
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	if rows[0].Cells[1].Value != before0 || rows[0].Cells[1].HTML != before0HTML {
		t.Fatalf("row 0 mutated with no Relations: got %+v want value=%q HTML=%v", rows[0].Cells[1], before0, before0HTML)
	}
	if rows[1].Cells[1].Value != before1 || rows[1].Cells[1].HTML != before1HTML {
		t.Fatalf("row 1 mutated with no Relations: got %+v want value=%q HTML=%v", rows[1].Cells[1], before1, before1HTML)
	}
}

// TestResolveRelations_BatchResolveLabels: ResolveLabels closure returns map
// -> cells replaced with CrossLinkCell HTML, Cell.HTML=true.
func TestResolveRelations_BatchResolveLabels(t *testing.T) {
	p := relPanel()
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
			return map[string]string{"42": "Alice", "43": "Bob"}, nil
		},
	})
	rows := []Row{postRow("1", "42"), postRow("2", "43")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	want0 := CrossLinkCell("/admin", "users", "42", "Alice")
	want1 := CrossLinkCell("/admin", "users", "43", "Bob")
	if rows[0].Cells[1].Value != want0 || !rows[0].Cells[1].HTML {
		t.Fatalf("row 0 author cell = %+v, want value=%q HTML=true", rows[0].Cells[1], want0)
	}
	if rows[1].Cells[1].Value != want1 || !rows[1].Cells[1].HTML {
		t.Fatalf("row 1 author cell = %+v, want value=%q HTML=true", rows[1].Cells[1], want1)
	}
}

// TestResolveRelations_FetchRowFallback_ScopedTarget: nil ResolveLabels,
// target has non-empty Scope, FetchRow returns DisplayKey -> cells replaced.
func TestResolveRelations_FetchRowFallback_ScopedTarget(t *testing.T) {
	target := usersTarget(tenant.Scope{Column: "u.city_slug"}, func(_ context.Context, id string) (map[string]string, error) {
		return map[string]string{"id": id, "name": "User-" + id}, nil
	})
	p := relPanel(target)
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
	})
	rows := []Row{postRow("1", "42"), postRow("2", "43")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	want0 := CrossLinkCell("/admin", "users", "42", "User-42")
	want1 := CrossLinkCell("/admin", "users", "43", "User-43")
	if rows[0].Cells[1].Value != want0 || !rows[0].Cells[1].HTML {
		t.Fatalf("row 0 author cell = %+v, want %q HTML=true", rows[0].Cells[1], want0)
	}
	if rows[1].Cells[1].Value != want1 || !rows[1].Cells[1].HTML {
		t.Fatalf("row 1 author cell = %+v, want %q HTML=true", rows[1].Cells[1], want1)
	}
}

// TestResolveRelations_FetchRowFallback_UnscopedTarget_Rejected: nil
// ResolveLabels, target has empty Scope -> raw FK preserved (ADR-3 tenant gate).
func TestResolveRelations_FetchRowFallback_UnscopedTarget_Rejected(t *testing.T) {
	called := false
	target := usersTarget(tenant.Scope{}, func(_ context.Context, id string) (map[string]string, error) {
		called = true
		return map[string]string{"name": "User-" + id}, nil
	})
	p := relPanel(target)
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
	})
	rows := []Row{postRow("1", "42")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	if called {
		t.Fatalf("FetchRow was called for unscoped target (ADR-3 tenant gate violated)")
	}
	if rows[0].Cells[1].Value != "42" || rows[0].Cells[1].HTML {
		t.Fatalf("raw FK not preserved for unscoped target: got %+v, want value=%q HTML=false", rows[0].Cells[1], "42")
	}
}

// TestResolveRelations_FetchRowFallback_MissingTarget: nil ResolveLabels,
// target resource not found -> raw FK preserved (ADR-6 runtime check).
func TestResolveRelations_FetchRowFallback_MissingTarget(t *testing.T) {
	p := relPanel() // no users target registered
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
	})
	rows := []Row{postRow("1", "42")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	if rows[0].Cells[1].Value != "42" || rows[0].Cells[1].HTML {
		t.Fatalf("raw FK not preserved for missing target: got %+v, want value=%q HTML=false", rows[0].Cells[1], "42")
	}
}

// TestResolveRelations_FetchRowFallback_CapExceeded: nil ResolveLabels,
// rows*relations > FALLBACK_CAP -> raw FK preserved (ADR-3 DoS cap).
func TestResolveRelations_FetchRowFallback_CapExceeded(t *testing.T) {
	called := 0
	target := usersTarget(tenant.Scope{Column: "u.city_slug"}, func(_ context.Context, id string) (map[string]string, error) {
		called++
		return map[string]string{"name": "User-" + id}, nil
	})
	p := relPanel(target)
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
	})
	// Build FALLBACK_CAP+1 distinct FK ids so a single relation exceeds the cap.
	rows := make([]Row, 0, FALLBACK_CAP+1)
	for i := 0; i < FALLBACK_CAP+1; i++ {
		fk := "id-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		rows = append(rows, postRow(fk, fk))
	}
	// Sanity: every FK is distinct and non-empty/non-zero.
	seen := map[string]bool{}
	for _, rw := range rows {
		fk := rw.Cells[1].Value
		if fk == "" || fk == "0" || seen[fk] {
			t.Fatalf("bad test fixture fk %q", fk)
		}
		seen[fk] = true
	}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	if called != 0 {
		t.Fatalf("FetchRow called %d times despite cap exceed (ADR-3 DoS cap violated)", called)
	}
	for i, rw := range rows {
		if rw.Cells[1].HTML {
			t.Fatalf("row %d FK replaced despite cap exceed: %+v", i, rw.Cells[1])
		}
	}
}

// TestResolveRelations_EmptyFKSkipped: FK value is "" or "0" -> no lookup,
// cell unchanged (ADR-10).
func TestResolveRelations_EmptyFKSkipped(t *testing.T) {
	p := relPanel()
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
			if len(ids) != 0 {
				t.Fatalf("expected zero ids when all FKs empty/zero, got %v", ids)
			}
			return map[string]string{}, nil
		},
	})
	rows := []Row{postRow("1", ""), postRow("2", "0"), postRow("3", "")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	for i, rw := range rows {
		if rw.Cells[1].HTML {
			t.Fatalf("row %d empty/zero FK replaced: %+v", i, rw.Cells[1])
		}
		if rw.Cells[1].Value != "" && rw.Cells[1].Value != "0" {
			t.Fatalf("row %d FK value mutated: got %q", i, rw.Cells[1].Value)
		}
	}
}

// TestResolveRelations_XSSLabelEscaped: ResolveLabels returns label with
// <script> -> CrossLinkCell escapes it (no raw <script> in output).
func TestResolveRelations_XSSLabelEscaped(t *testing.T) {
	p := relPanel()
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
			return map[string]string{"42": "<script>alert(1)</script>"}, nil
		},
	})
	rows := []Row{postRow("1", "42")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	got := rows[0].Cells[1].Value
	if strings.Contains(got, "<script>") {
		t.Fatalf("output contains raw <script> tag (stored XSS): %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("label was not HTML-escaped as expected: %q", got)
	}
}

// TestResolveRelations_MissingIDInMap_LeavesRawFK: ResolveLabels returns map
// missing some IDs -> those rows keep raw FK (ADR-9 graceful degradation).
func TestResolveRelations_MissingIDInMap_LeavesRawFK(t *testing.T) {
	p := relPanel()
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
			// Only resolve id 42; 43 is missing.
			return map[string]string{"42": "Alice"}, nil
		},
	})
	rows := []Row{postRow("1", "42"), postRow("2", "43")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	want0 := CrossLinkCell("/admin", "users", "42", "Alice")
	if rows[0].Cells[1].Value != want0 || !rows[0].Cells[1].HTML {
		t.Fatalf("row 0 (resolved id) = %+v, want %q HTML=true", rows[0].Cells[1], want0)
	}
	if rows[1].Cells[1].Value != "43" || rows[1].Cells[1].HTML {
		t.Fatalf("row 1 (missing id) raw FK not preserved: got %+v, want value=%q HTML=false", rows[1].Cells[1], "43")
	}
}

// TestResolveRelations_ErrorInResolveLabels_LeavesRawFK: ResolveLabels returns
// error -> raw FK preserved (ADR-9).
func TestResolveRelations_ErrorInResolveLabels_LeavesRawFK(t *testing.T) {
	p := relPanel()
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
			return nil, errors.New("boom")
		},
	})
	rows := []Row{postRow("1", "42"), postRow("2", "43")}
	if err := resolveRelations(context.Background(), p, r, rows); err != nil {
		t.Fatalf("resolveRelations returned error: %v", err)
	}
	for i, rw := range rows {
		if rw.Cells[1].HTML {
			t.Fatalf("row %d FK replaced despite ResolveLabels error: %+v", i, rw.Cells[1])
		}
	}
	if rows[0].Cells[1].Value != "42" || rows[1].Cells[1].Value != "43" {
		t.Fatalf("raw FK values not preserved on ResolveLabels error: got %q %q", rows[0].Cells[1].Value, rows[1].Cells[1].Value)
	}
}

// TestValidateRelationsConfig_ForeignKeyMismatch_Panics: ForeignKey doesn't
// match any Sort.Columns[].Key -> panic.
func TestValidateRelationsConfig_ForeignKeyMismatch_Panics(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "nonexistent_key",
		DisplayKey: "name",
	})
	defer func() {
		if recover() == nil {
			t.Fatalf("validateRelationsConfig did not panic on mismatched ForeignKey")
		}
	}()
	validateRelationsConfig(r)
}

// TestValidateRelationsConfig_ValidForeignKey_NoPanic: ForeignKey matches ->
// no panic.
func TestValidateRelationsConfig_ValidForeignKey_NoPanic(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
	})
	defer func() {
		if v := recover(); v != nil {
			t.Fatalf("validateRelationsConfig panicked on valid ForeignKey: %v", v)
		}
	}()
	validateRelationsConfig(r)
}
