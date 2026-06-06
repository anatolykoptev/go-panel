package admintable_test

import (
	"net/url"
	"testing"

	"github.com/anatolykoptev/go-panel/admintable"
)

func TestSpec_Valid(t *testing.T) {
	t.Run("valid spec", func(t *testing.T) {
		sp := admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Sortable: true, SQLExpr: "p.name"},
				{Key: "created", Sortable: true, SQLExpr: "p.created_at", NullsLast: true},
			},
			DefaultKey: "name",
			DefaultDir: admintable.Asc,
		}
		if err := sp.Valid(); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("no sortable columns", func(t *testing.T) {
		sp := admintable.Spec{
			Columns:    []admintable.Column{{Key: "name", SQLExpr: "p.name"}},
			DefaultKey: "name",
		}
		if err := sp.Valid(); err == nil {
			t.Fatal("expected error for no sortable columns")
		}
	})

	t.Run("default key not sortable", func(t *testing.T) {
		sp := admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Sortable: true, SQLExpr: "p.name"},
				{Key: "other", SQLExpr: "p.other"},
			},
			DefaultKey: "other",
		}
		if err := sp.Valid(); err == nil {
			t.Fatal("expected error for default key not sortable")
		}
	})

	t.Run("duplicate keys", func(t *testing.T) {
		sp := admintable.Spec{
			Columns: []admintable.Column{
				{Key: "name", Sortable: true, SQLExpr: "p.name"},
				{Key: "name", Sortable: true, SQLExpr: "p.name2"},
			},
			DefaultKey: "name",
		}
		if err := sp.Valid(); err == nil {
			t.Fatal("expected error for duplicate keys")
		}
	})
}

func TestSpec_Resolve(t *testing.T) {
	sp := admintable.Spec{
		Columns: []admintable.Column{
			{Key: "name", Sortable: true, SQLExpr: "p.name"},
			{Key: "created", Sortable: true, SQLExpr: "p.created_at"},
		},
		DefaultKey: "name",
		DefaultDir: admintable.Desc,
	}

	t.Run("defaults on empty input", func(t *testing.T) {
		st := sp.Resolve("", "")
		if st.Key != "name" || st.Dir != admintable.Desc {
			t.Errorf("got %v, want {name desc}", st)
		}
	})

	t.Run("valid sort key accepted", func(t *testing.T) {
		st := sp.Resolve("created", "asc")
		if st.Key != "created" || st.Dir != admintable.Asc {
			t.Errorf("got %v", st)
		}
	})

	t.Run("injected key rejected", func(t *testing.T) {
		st := sp.Resolve("1; DROP TABLE places--", "asc")
		if st.Key != "name" {
			t.Errorf("expected default key, got %q", st.Key)
		}
	})

	t.Run("injected dir rejected", func(t *testing.T) {
		st := sp.Resolve("name", "asc; DROP TABLE")
		if st.Dir != admintable.Desc {
			t.Errorf("expected default dir, got %q", st.Dir)
		}
	})
}

func TestSpec_OrderBy(t *testing.T) {
	sp := admintable.Spec{
		Columns: []admintable.Column{
			{Key: "name", Sortable: true, SQLExpr: "p.name"},
			{Key: "updated", Sortable: true, SQLExpr: "p.updated_at", NullsLast: true},
		},
		DefaultKey: "name",
		DefaultDir: admintable.Asc,
	}

	t.Run("asc", func(t *testing.T) {
		st := sp.Resolve("name", "asc")
		ob := sp.OrderBy(st)
		if ob != "p.name ASC" {
			t.Errorf("got %q", ob)
		}
	})

	t.Run("desc with nulls last", func(t *testing.T) {
		st := sp.Resolve("updated", "desc")
		ob := sp.OrderBy(st)
		if ob != "p.updated_at DESC NULLS LAST" {
			t.Errorf("got %q", ob)
		}
	})
}

func TestFilterSpec_Valid(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		fs := admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "status", SQLExpr: "p.status", Match: admintable.Eq, Allowed: []string{"published", "draft"}},
		}}
		if err := fs.Valid(); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})

	t.Run("duplicate keys", func(t *testing.T) {
		fs := admintable.FilterSpec{Filters: []admintable.Filter{
			{Key: "q", SQLExpr: "p.name", Match: admintable.ILike},
			{Key: "q", SQLExpr: "p.title", Match: admintable.ILike},
		}}
		if err := fs.Valid(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFilterSpec_Where(t *testing.T) {
	fs := admintable.FilterSpec{Filters: []admintable.Filter{
		{Key: "status", SQLExpr: "p.status", Match: admintable.Eq, Allowed: []string{"published", "draft"}},
		{Key: "q", SQLExpr: "p.name", Match: admintable.ILike},
	}}

	t.Run("no params — empty output", func(t *testing.T) {
		conds, args := fs.Where(url.Values{}, 1)
		if conds != "" || len(args) != 0 {
			t.Errorf("expected empty, got %q %v", conds, args)
		}
	})

	t.Run("eq filter accepted", func(t *testing.T) {
		conds, args := fs.Where(url.Values{"status": {"published"}}, 1)
		if conds != "p.status = $1" {
			t.Errorf("got %q", conds)
		}
		if len(args) != 1 || args[0] != "published" {
			t.Errorf("got args %v", args)
		}
	})

	t.Run("disallowed value rejected", func(t *testing.T) {
		conds, args := fs.Where(url.Values{"status": {"injected'; DROP TABLE--"}}, 1)
		if conds != "" || len(args) != 0 {
			t.Errorf("expected empty for disallowed value, got %q %v", conds, args)
		}
	})

	t.Run("ilike filter", func(t *testing.T) {
		conds, args := fs.Where(url.Values{"q": {"coffee"}}, 1)
		if conds != "p.name ILIKE $1" {
			t.Errorf("got %q", conds)
		}
		if len(args) != 1 || args[0] != "%coffee%" {
			t.Errorf("got args %v", args)
		}
	})

	t.Run("multiple filters combined with AND", func(t *testing.T) {
		conds, args := fs.Where(url.Values{"status": {"draft"}, "q": {"bar"}}, 1)
		if !contains(conds, "AND") {
			t.Errorf("expected AND in %q", conds)
		}
		if len(args) != 2 {
			t.Errorf("expected 2 args, got %v", args)
		}
	})

	t.Run("startArg offset applied", func(t *testing.T) {
		conds, _ := fs.Where(url.Values{"status": {"published"}}, 5)
		if conds != "p.status = $5" {
			t.Errorf("got %q", conds)
		}
	})
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := range s {
		if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
