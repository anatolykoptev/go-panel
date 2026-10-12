package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
)

// seedFake records every AccountStore call so tests can assert "touched nothing".
type seedFake struct {
	getErr    error
	existing  *auth.Account
	createErr error
	createID  string
	created   bool

	getCalls, createCalls, updateCalls  int
	gotEmail, gotName, gotHash, gotRole string
}

func (f *seedFake) GetByEmail(_ context.Context, email string) (*auth.Account, error) {
	f.getCalls++
	f.gotEmail = email
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.existing != nil {
		return f.existing, nil
	}
	return nil, auth.ErrAccountNotFound
}
func (f *seedFake) GetByID(context.Context, string) (*auth.Account, error) {
	return nil, auth.ErrAccountNotFound
}
func (f *seedFake) UpdateLastLogin(context.Context, string) error { return nil }
func (f *seedFake) UpdatePasswordHash(context.Context, string, string) error {
	f.updateCalls++
	return nil
}
func (f *seedFake) CreateAccount(_ context.Context, email, name, hash, role string) (string, bool, error) {
	f.createCalls++
	f.gotEmail, f.gotName, f.gotHash, f.gotRole = email, name, hash, role
	return f.createID, f.created, f.createErr
}

func TestSeedAccount_RejectsInvalidInputWithoutTouchingStore(t *testing.T) {
	cases := []struct{ name, email, password string }{
		{"empty password", "ops@example.com", ""},
		{"empty email", "", "pw"},
		{"no at", "ops.example.com", "pw"},
		{"empty local", "@example.com", "pw"},
		{"empty domain", "ops@", "pw"},
		{"two ats", "a@b@example.com", "pw"},
		{"double at", "a@@example.com", "pw"},
		{"only at", "@", "pw"},
		{"whitespace email", "   ", "pw"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &seedFake{}
			id, created, err := auth.SeedAccount(context.Background(), f, c.email, c.password, "Ops", auth.RoleOwner)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if id != "" || created {
				t.Fatalf("got (%q, %v), want zero values on error", id, created)
			}
			if f.getCalls+f.createCalls+f.updateCalls != 0 {
				t.Fatalf("store touched: get=%d create=%d update=%d", f.getCalls, f.createCalls, f.updateCalls)
			}
		})
	}
}

func TestSeedAccount_ExistingAccountIsNeverOverwritten(t *testing.T) {
	f := &seedFake{existing: &auth.Account{ID: "acc-1", Email: "ops@example.com", PasswordHash: "old-hash", Active: true}}
	id, created, err := auth.SeedAccount(context.Background(), f, "ops@example.com", "new-password", "Ops", auth.RoleOwner)
	if err != nil {
		t.Fatalf("SeedAccount: %v", err)
	}
	if id != "acc-1" || created {
		t.Fatalf("got (%q, %v), want (acc-1, false)", id, created)
	}
	if f.updateCalls != 0 {
		t.Fatalf("UpdatePasswordHash called %d time(s): an existing hash must never be overwritten", f.updateCalls)
	}
	if f.createCalls != 0 {
		t.Fatalf("CreateAccount called %d time(s) for an existing account", f.createCalls)
	}
}

func TestSeedAccount_CreatesWhenMissing(t *testing.T) {
	f := &seedFake{createID: "new-id", created: true}
	id, created, err := auth.SeedAccount(context.Background(), f, "  Ops@Example.COM ", "s3cret-pw", "Ops", auth.RoleOwner)
	if err != nil {
		t.Fatalf("SeedAccount: %v", err)
	}
	if id != "new-id" || !created {
		t.Fatalf("got (%q, %v), want (new-id, true)", id, created)
	}
	if f.gotEmail != "ops@example.com" {
		t.Fatalf("email = %q, want normalized ops@example.com", f.gotEmail)
	}
	if f.gotName != "Ops" || f.gotRole != auth.RoleOwner {
		t.Fatalf("name/role = %q/%q", f.gotName, f.gotRole)
	}
	if f.gotHash == "s3cret-pw" || !auth.VerifyPassword("s3cret-pw", f.gotHash) {
		t.Fatalf("stored hash %q is not a bcrypt hash of the password", f.gotHash)
	}
	if f.updateCalls != 0 {
		t.Fatalf("UpdatePasswordHash called %d time(s) on the create path", f.updateCalls)
	}
}

func TestSeedAccount_CreateReportsExistingRowIsNotCreated(t *testing.T) {
	// GetByEmail only sees ACTIVE accounts; a deactivated row (or a lost race)
	// surfaces as CreateAccount -> created=false. That must pass through as a
	// no-op, not an error and not a resurrection.
	f := &seedFake{createID: "", created: false}
	id, created, err := auth.SeedAccount(context.Background(), f, "ops@example.com", "pw", "Ops", auth.RoleOwner)
	if err != nil {
		t.Fatalf("SeedAccount: %v", err)
	}
	if created || id != "" {
		t.Fatalf("got (%q, %v), want (\"\", false)", id, created)
	}
	if f.updateCalls != 0 {
		t.Fatal("UpdatePasswordHash must never run")
	}
}

func TestSeedAccount_LookupErrorAborts(t *testing.T) {
	boom := errors.New("db down")
	f := &seedFake{getErr: boom}
	_, created, err := auth.SeedAccount(context.Background(), f, "ops@example.com", "pw", "Ops", auth.RoleOwner)
	if !errors.Is(err, boom) || created {
		t.Fatalf("got (created=%v, err=%v), want wrapped %v", created, err, boom)
	}
	if f.createCalls != 0 {
		t.Fatal("must not create after a failed lookup")
	}
}

func TestSeedAccount_CreateErrorPropagates(t *testing.T) {
	boom := errors.New("insert failed")
	f := &seedFake{createErr: boom}
	_, created, err := auth.SeedAccount(context.Background(), f, "ops@example.com", "pw", "Ops", auth.RoleOwner)
	if !errors.Is(err, boom) || created {
		t.Fatalf("got (created=%v, err=%v), want wrapped %v", created, err, boom)
	}
}

func TestSeedAccount_UnhashablePasswordAborts(t *testing.T) {
	f := &seedFake{}
	// bcrypt rejects > 72 bytes; HashPassword's error must surface, not be swallowed.
	_, _, err := auth.SeedAccount(context.Background(), f, "ops@example.com", strings.Repeat("x", 73), "Ops", auth.RoleOwner)
	if err == nil {
		t.Fatal("want hash error for a 73-byte password")
	}
	if f.createCalls != 0 {
		t.Fatal("must not create when hashing failed")
	}
}

func TestSeedAccount_RejectsEmptyRoleWithoutTouchingStore(t *testing.T) {
	for _, role := range []string{"", "   ", "\t\n"} {
		f := &seedFake{}
		_, created, err := auth.SeedAccount(context.Background(), f, "ops@example.com", "pw", "Ops", role)
		if !errors.Is(err, auth.ErrInvalidSeed) || created {
			t.Fatalf("role %q: got (created=%v, err=%v), want ErrInvalidSeed", role, created, err)
		}
		if f.getCalls+f.createCalls+f.updateCalls != 0 {
			t.Fatalf("role %q: store touched", role)
		}
	}
}
