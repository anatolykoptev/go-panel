package auth_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

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
		// CASCADE: EnsureSchema also creates panel_totp_recovery_codes,
		// which FK-references panel_accounts (see totpSchemaSQL) -- a bare
		// DROP TABLE panel_accounts would fail once that child table
		// exists.
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS panel_accounts CASCADE")
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

// setupTOTPTestStore returns a ready PgxAccountStore (schema ensured,
// including the TOTP additions) and a freshly created account ID to
// exercise TOTPStore against. Skips the test if TEST_DATABASE_URL is not
// set — the exact same DSN-gated skip convention as
// TestPgxAccountStore_RoundTrip above, not a new pattern.
func setupTOTPTestStore(t *testing.T) (*auth.PgxAccountStore, context.Context, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PG integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	store := auth.NewPgxAccountStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS panel_accounts CASCADE")
	})

	hash, _ := auth.HashPassword("s3cret-pw")
	email := fmt.Sprintf("totp-%d@example.com", time.Now().UnixNano())
	id, created, err := store.CreateAccount(ctx, email, "TOTP Test Operator", hash, "admin")
	if err != nil || !created || id == "" {
		t.Fatalf("CreateAccount: id=%q created=%v err=%v", id, created, err)
	}
	return store, ctx, id
}

// TestPgxAccountStore_TOTPEnrollmentLifecycle exercises the full
// set-pending -> confirm -> disable sequence the schema was designed for
// (see account_pgx.go's SetPendingTOTPSecret doc), asserting
// Account.TOTPEnabled and GetTOTPSecret's visibility at each stage.
func TestPgxAccountStore_TOTPEnrollmentLifecycle(t *testing.T) {
	store, ctx, id := setupTOTPTestStore(t)

	if _, err := store.GetTOTPSecret(ctx, id); !errors.Is(err, auth.ErrTOTPNotEnrolled) {
		t.Fatalf("GetTOTPSecret before enrollment: got err=%v, want ErrTOTPNotEnrolled", err)
	}

	encrypted := []byte("fake-encrypted-secret-bytes")
	if err := store.SetPendingTOTPSecret(ctx, id, encrypted); err != nil {
		t.Fatalf("SetPendingTOTPSecret: %v", err)
	}

	got, err := store.GetTOTPSecret(ctx, id)
	if err != nil {
		t.Fatalf("GetTOTPSecret (pending): %v", err)
	}
	if !bytes.Equal(got, encrypted) {
		t.Fatalf("GetTOTPSecret (pending) = %q, want %q", got, encrypted)
	}
	acct, err := store.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID (pending): %v", err)
	}
	if acct.TOTPEnabled {
		t.Fatal("TOTPEnabled must stay false after SetPendingTOTPSecret, before Confirm")
	}

	if err := store.ConfirmTOTPEnrollment(ctx, id); err != nil {
		t.Fatalf("ConfirmTOTPEnrollment: %v", err)
	}
	acct, err = store.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID (confirmed): %v", err)
	}
	if !acct.TOTPEnabled {
		t.Fatal("TOTPEnabled must be true after ConfirmTOTPEnrollment")
	}

	if err := store.DisableTOTP(ctx, id); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	acct, err = store.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID (disabled): %v", err)
	}
	if acct.TOTPEnabled {
		t.Fatal("TOTPEnabled must be false after DisableTOTP")
	}
	if _, err := store.GetTOTPSecret(ctx, id); !errors.Is(err, auth.ErrTOTPNotEnrolled) {
		t.Fatalf("GetTOTPSecret after DisableTOTP: got err=%v, want ErrTOTPNotEnrolled", err)
	}
}

// TestPgxAccountStore_ConsumeTOTPStep_ReplayGuard falsifies the exact
// invariant the spec calls out: the SAME step (or an OLDER one) must be
// rejected, a LATER step must succeed.
func TestPgxAccountStore_ConsumeTOTPStep_ReplayGuard(t *testing.T) {
	store, ctx, id := setupTOTPTestStore(t)

	ok, err := store.ConsumeTOTPStep(ctx, id, 100)
	if err != nil {
		t.Fatalf("ConsumeTOTPStep(100): %v", err)
	}
	if !ok {
		t.Fatal("ConsumeTOTPStep(100): expected ok=true on first use")
	}

	if ok, err = store.ConsumeTOTPStep(ctx, id, 100); err != nil {
		t.Fatalf("ConsumeTOTPStep(100) again: %v", err)
	} else if ok {
		t.Fatal("ConsumeTOTPStep(100) a second time: expected ok=false (replay), got true")
	}

	if ok, err = store.ConsumeTOTPStep(ctx, id, 99); err != nil {
		t.Fatalf("ConsumeTOTPStep(99): %v", err)
	} else if ok {
		t.Fatal("ConsumeTOTPStep(99) after 100: expected ok=false (older step), got true")
	}

	if ok, err = store.ConsumeTOTPStep(ctx, id, 101); err != nil {
		t.Fatalf("ConsumeTOTPStep(101): %v", err)
	} else if !ok {
		t.Fatal("ConsumeTOTPStep(101) after 100: expected ok=true, got false")
	}
}

// TestPgxAccountStore_RecoveryCodes_SingleUse falsifies single-use: a
// stored code consumes exactly once, an unrelated hash never consumes, and
// untouched sibling codes remain valid.
func TestPgxAccountStore_RecoveryCodes_SingleUse(t *testing.T) {
	store, ctx, id := setupTOTPTestStore(t)

	_, hashes, err := auth.GenerateRecoveryCodes(3)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if err := store.StoreRecoveryCodes(ctx, id, hashes); err != nil {
		t.Fatalf("StoreRecoveryCodes: %v", err)
	}
	target := hashes[1]

	if ok, err := store.ConsumeRecoveryCode(ctx, id, target); err != nil || !ok {
		t.Fatalf("ConsumeRecoveryCode (first use): ok=%v err=%v, want ok=true", ok, err)
	}
	if ok, err := store.ConsumeRecoveryCode(ctx, id, target); err != nil {
		t.Fatalf("ConsumeRecoveryCode (replay): %v", err)
	} else if ok {
		t.Fatal("ConsumeRecoveryCode (replay): expected ok=false, code already used")
	}

	unknown := auth.HashRecoveryCode("ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZ")
	if ok, err := store.ConsumeRecoveryCode(ctx, id, unknown); err != nil {
		t.Fatalf("ConsumeRecoveryCode (unknown): %v", err)
	} else if ok {
		t.Fatal("ConsumeRecoveryCode (unknown code): expected ok=false")
	}

	for _, h := range [][]byte{hashes[0], hashes[2]} {
		if ok, err := store.ConsumeRecoveryCode(ctx, id, h); err != nil || !ok {
			t.Fatalf("ConsumeRecoveryCode (untouched sibling code): ok=%v err=%v, want ok=true", ok, err)
		}
	}
}

// TestPgxAccountStore_StoreRecoveryCodes_ReplacesPriorSet falsifies the
// interface's documented "replaces any existing set" contract.
func TestPgxAccountStore_StoreRecoveryCodes_ReplacesPriorSet(t *testing.T) {
	store, ctx, id := setupTOTPTestStore(t)

	_, firstHashes, err := auth.GenerateRecoveryCodes(2)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if err := store.StoreRecoveryCodes(ctx, id, firstHashes); err != nil {
		t.Fatalf("StoreRecoveryCodes (first): %v", err)
	}

	_, secondHashes, err := auth.GenerateRecoveryCodes(2)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if err := store.StoreRecoveryCodes(ctx, id, secondHashes); err != nil {
		t.Fatalf("StoreRecoveryCodes (second): %v", err)
	}

	for _, h := range firstHashes {
		if ok, err := store.ConsumeRecoveryCode(ctx, id, h); err != nil {
			t.Fatalf("ConsumeRecoveryCode (stale code): %v", err)
		} else if ok {
			t.Fatal("a code from the REPLACED first set was still consumable")
		}
	}
	for _, h := range secondHashes {
		if ok, err := store.ConsumeRecoveryCode(ctx, id, h); err != nil || !ok {
			t.Fatalf("ConsumeRecoveryCode (current code): ok=%v err=%v, want ok=true", ok, err)
		}
	}
}

// TestPgxAccountStore_DisableTOTP_ClearsRecoveryCodes falsifies the
// transactional cleanup: a recovery code stored before DisableTOTP must
// never consume afterward.
func TestPgxAccountStore_DisableTOTP_ClearsRecoveryCodes(t *testing.T) {
	store, ctx, id := setupTOTPTestStore(t)

	_, hashes, err := auth.GenerateRecoveryCodes(2)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if err := store.StoreRecoveryCodes(ctx, id, hashes); err != nil {
		t.Fatalf("StoreRecoveryCodes: %v", err)
	}
	if err := store.DisableTOTP(ctx, id); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}

	for _, h := range hashes {
		if ok, err := store.ConsumeRecoveryCode(ctx, id, h); err != nil {
			t.Fatalf("ConsumeRecoveryCode after DisableTOTP: %v", err)
		} else if ok {
			t.Fatal("a recovery code survived DisableTOTP -- must be purged, not just orphaned")
		}
	}
}

// TestPgxAccountStore_TOTPStore_UnknownAccountNotFound covers the
// not-found path across every TOTPStore write/read method.
func TestPgxAccountStore_TOTPStore_UnknownAccountNotFound(t *testing.T) {
	store, ctx, _ := setupTOTPTestStore(t)
	const bogus = "00000000-0000-0000-0000-000000000000" // syntactically valid UUID, no such row

	if err := store.SetPendingTOTPSecret(ctx, bogus, []byte("x")); !errors.Is(err, auth.ErrAccountNotFound) {
		t.Errorf("SetPendingTOTPSecret(bogus): got %v, want ErrAccountNotFound", err)
	}
	if err := store.ConfirmTOTPEnrollment(ctx, bogus); !errors.Is(err, auth.ErrAccountNotFound) {
		t.Errorf("ConfirmTOTPEnrollment(bogus): got %v, want ErrAccountNotFound", err)
	}
	if _, err := store.GetTOTPSecret(ctx, bogus); !errors.Is(err, auth.ErrAccountNotFound) {
		t.Errorf("GetTOTPSecret(bogus): got %v, want ErrAccountNotFound", err)
	}
	if err := store.DisableTOTP(ctx, bogus); !errors.Is(err, auth.ErrAccountNotFound) {
		t.Errorf("DisableTOTP(bogus): got %v, want ErrAccountNotFound", err)
	}
}
