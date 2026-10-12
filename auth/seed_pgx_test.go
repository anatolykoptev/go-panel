package auth_test

import (
	"context"
	"os"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSeedAccount_Pgx_DeactivatedAccountNotResurrected runs SeedAccount against
// the real PgxAccountStore. GetByEmail only returns ACTIVE rows, so a
// deactivated operator looks "missing" to the first lookup; this proves
// CreateAccount's ON CONFLICT DO NOTHING keeps the seed a no-op.
// Same TEST_DATABASE_URL gate as account_test.go.
func TestSeedAccount_Pgx_DeactivatedAccountNotResurrected(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PG integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
	store := auth.NewPgxAccountStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS panel_accounts CASCADE")
	})

	id, created, err := auth.SeedAccount(ctx, store, "Seed.Owner@Example.com", "first-pw", "Owner", auth.RoleOwner)
	if err != nil || !created || id == "" {
		t.Fatalf("first seed: id=%q created=%v err=%v", id, created, err)
	}
	var hashBefore string
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM panel_accounts WHERE id=$1", id).Scan(&hashBefore); err != nil {
		t.Fatalf("select hash: %v", err)
	}

	// Active account: re-seed with a different password is a pure no-op.
	id2, created2, err := auth.SeedAccount(ctx, store, "seed.owner@example.com", "other-pw", "Owner", auth.RoleOwner)
	if err != nil || created2 || id2 != id {
		t.Fatalf("active re-seed: id=%q created=%v err=%v, want (%q,false,nil)", id2, created2, err, id)
	}

	// Deactivated account: must stay deactivated with its original hash.
	if err := store.SetActive(ctx, id, false); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if _, created3, err := auth.SeedAccount(ctx, store, "seed.owner@example.com", "other-pw", "Owner", auth.RoleOwner); err != nil || created3 {
		t.Fatalf("deactivated re-seed: created=%v err=%v, want (false,nil)", created3, err)
	}
	var active bool
	var hashAfter string
	var rows int
	if err := pool.QueryRow(ctx, "SELECT active, password_hash FROM panel_accounts WHERE id=$1", id).Scan(&active, &hashAfter); err != nil {
		t.Fatalf("select state: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM panel_accounts").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if active {
		t.Error("deactivated account was resurrected by SeedAccount")
	}
	if hashAfter != hashBefore {
		t.Error("deactivated account's password hash was overwritten by SeedAccount")
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1 (no duplicate account)", rows)
	}
}
