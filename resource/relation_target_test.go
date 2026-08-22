package resource

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The case ResolveLabels cannot express: the foreign key is not the target's
// ID. The label resolved fine before this hook existed; the href was built
// from the FK, so it pointed at a row that does not exist.
//
// Measured on go-grad: five resources referenced a business by its integer
// merchant_id while the clients resource is keyed by an org UUID, so the cell
// read "Детская академия наук" and linked to /admin/clients/1.
//
// RED-on-revert: drop the ResolveTargets branch from resolveTargets and the
// href falls back to the FK.
func TestResolveRelations_ResolveTargets_LinksToTheTargetID(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveTargets: func(_ context.Context, ids []string) (map[string]RelationTarget, error) {
			out := map[string]RelationTarget{}
			for _, id := range ids {
				if id == "1" {
					out[id] = RelationTarget{ID: "7e55f17f-uuid", Label: "Acme"}
				}
			}
			return out, nil
		},
	})
	rows := []Row{postRow("p1", "1")}

	if err := resolveRelations(context.Background(), relPanel(*r), r, rows); err != nil {
		t.Fatalf("resolveRelations: %v", err)
	}

	got := rows[0].Cells[1].Value
	if !strings.Contains(got, `href="/admin/users/7e55f17f-uuid"`) {
		t.Errorf("href built from the FK, not the target ID:\n%s", got)
	}
	if strings.Contains(got, `href="/admin/users/1"`) {
		t.Errorf("href still points at the foreign key:\n%s", got)
	}
	if !strings.Contains(got, "Acme") {
		t.Errorf("label lost: %s", got)
	}
}

// A name with no row behind it. Before this, an empty ID would have produced
// an anchor to the resource root — a cell that reads as a working link and
// goes somewhere wrong on click. Plain text says the same thing honestly.
//
// This is not hypothetical: a merchant with no org row resolves to a business
// name and to no client page.
func TestResolveRelations_ResolveTargets_EmptyIDRendersPlainText(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveTargets: func(_ context.Context, ids []string) (map[string]RelationTarget, error) {
			out := map[string]RelationTarget{}
			for _, id := range ids {
				out[id] = RelationTarget{ID: "", Label: "Nameless Co"}
			}
			return out, nil
		},
	})
	rows := []Row{postRow("p1", "42")}

	if err := resolveRelations(context.Background(), relPanel(*r), r, rows); err != nil {
		t.Fatalf("resolveRelations: %v", err)
	}

	cell := rows[0].Cells[1]
	if strings.Contains(cell.Value, "<a ") {
		t.Errorf("a target with no ID still rendered an anchor: %s", cell.Value)
	}
	if cell.HTML {
		t.Errorf("plain-text fallback must not be marked HTML — the label goes through escaping")
	}
	if cell.Value != "Nameless Co" {
		t.Errorf("label = %q, want %q", cell.Value, "Nameless Co")
	}
}

// The fifteen relations that were already correct must not move. Where the FK
// IS the target's ID, ResolveLabels keeps building the same href it always
// did — this is the regression the new hook could most easily have caused.
func TestResolveRelations_ResolveLabelsOnly_Unchanged(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, ids []string) (map[string]string, error) {
			out := map[string]string{}
			for _, id := range ids {
				out[id] = "User " + id
			}
			return out, nil
		},
	})
	rows := []Row{postRow("p1", "7")}

	if err := resolveRelations(context.Background(), relPanel(*r), r, rows); err != nil {
		t.Fatalf("resolveRelations: %v", err)
	}

	got := rows[0].Cells[1].Value
	if !strings.Contains(got, `href="/admin/users/7"`) {
		t.Errorf("identity relation changed shape:\n%s", got)
	}
	if !strings.Contains(got, "User 7") {
		t.Errorf("label lost: %s", got)
	}
}

// ResolveTargets wins when both are declared, so a consumer migrating one
// relation at a time cannot end up with the old href silently winning.
func TestResolveRelations_ResolveTargets_WinsOverResolveLabels(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveLabels: func(_ context.Context, _ []string) (map[string]string, error) {
			return map[string]string{"1": "from ResolveLabels"}, nil
		},
		ResolveTargets: func(_ context.Context, _ []string) (map[string]RelationTarget, error) {
			return map[string]RelationTarget{"1": {ID: "uuid-x", Label: "from ResolveTargets"}}, nil
		},
	})
	rows := []Row{postRow("p1", "1")}

	if err := resolveRelations(context.Background(), relPanel(*r), r, rows); err != nil {
		t.Fatalf("resolveRelations: %v", err)
	}
	got := rows[0].Cells[1].Value
	if !strings.Contains(got, "from ResolveTargets") || !strings.Contains(got, "uuid-x") {
		t.Errorf("ResolveLabels won over ResolveTargets:\n%s", got)
	}
}

// A failing resolver leaves the raw FK, exactly as ResolveLabels does. An
// operator can still read the id; a blank cell tells them nothing.
func TestResolveRelations_ResolveTargets_ErrorLeavesRawFK(t *testing.T) {
	r := postsResource(Relation{
		Resource:   "users",
		ForeignKey: "author_id",
		DisplayKey: "name",
		ResolveTargets: func(_ context.Context, _ []string) (map[string]RelationTarget, error) {
			return nil, errors.New("upstream down")
		},
	})
	rows := []Row{postRow("p1", "9")}

	if err := resolveRelations(context.Background(), relPanel(*r), r, rows); err != nil {
		t.Fatalf("resolveRelations: %v", err)
	}
	if got := rows[0].Cells[1].Value; got != "9" {
		t.Errorf("cell = %q, want the raw FK %q", got, "9")
	}
}
