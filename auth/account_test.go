package auth_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHashVerifyPassword(t *testing.T) {
	const pw = "correct horse battery staple"
	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == pw {
		t.Fatal("hash must not equal plaintext")
	}
	if !auth.VerifyPassword(pw, hash) {
		t.Error("VerifyPassword rejected the correct password")
	}
	if auth.VerifyPassword("wrong", hash) {
		t.Error("VerifyPassword accepted a wrong password")
	}
	if auth.VerifyPassword("x", "not-a-bcrypt-hash") {
		t.Error("VerifyPassword accepted against a malformed hash")
	}
}

// TestPgxAccountStore_RoundTrip exercises the store against a real Postgres.
// It is SKIPPED unless TEST_DATABASE_URL is set (never DATABASE_URL / prod).
// Point TEST_DATABASE_URL at a throwaway database; the test drops its table on cleanup.
func TestPgxAccountStore_RoundTrip(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS panel_accounts")
	})

	hash, _ := auth.HashPassword("s3cret-pw")
	id, created, err := store.CreateAccount(ctx, "op@example.com", "Operator", hash, "admin")
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if !created || id == "" {
		t.Fatalf("expected created account with id, got created=%v id=%q", created, id)
	}

	if _, created2, err := store.CreateAccount(ctx, "op@example.com", "Operator", hash, "admin"); err != nil || created2 {
		t.Fatalf("expected idempotent no-op on duplicate email, got created=%v err=%v", created2, err)
	}

	got, err := store.GetByEmail(ctx, "op@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != id || got.Role != "admin" || !got.Active {
		t.Fatalf("GetByEmail mismatch: %+v", got)
	}
	if !auth.VerifyPassword("s3cret-pw", got.PasswordHash) {
		t.Error("stored password hash does not verify")
	}

	byID, err := store.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if byID.Email != "op@example.com" {
		t.Fatalf("GetByID mismatch: %+v", byID)
	}

	if err := store.UpdateLastLogin(ctx, id); err != nil {
		t.Errorf("UpdateLastLogin: %v", err)
	}

	newHash, _ := auth.HashPassword("n3w-pw")
	if err := store.UpdatePasswordHash(ctx, id, newHash); err != nil {
		t.Errorf("UpdatePasswordHash: %v", err)
	}
	after, _ := store.GetByEmail(ctx, "op@example.com")
	if !auth.VerifyPassword("n3w-pw", after.PasswordHash) {
		t.Error("password hash not updated")
	}

	if _, err := store.GetByEmail(ctx, "nobody@example.com"); !errors.Is(err, auth.ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}
