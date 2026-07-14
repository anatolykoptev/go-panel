package auth_test

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/pquerna/otp/totp"
)

// domainTOTPStore is an in-memory AccountStore+TOTPStore with REAL
// (not no-op) semantics mirroring account_pgx.go's documented contract --
// distinct from bcrypt_auth_test.go's fakeTOTPStore, whose methods are
// deliberately unused stubs for a different test suite (panic-wiring only).
// This phase's domain functions need genuine enroll/confirm/replay/disable
// behavior to exercise for real.
type domainTOTPStore struct {
	accounts map[string]*auth.Account
	byEmail  map[string]*auth.Account
	pending  map[string][]byte
	lastStep map[string]int64
	recovery map[string]map[string]bool // accountID -> hash string -> used
}

func newDomainTOTPStore() *domainTOTPStore {
	return &domainTOTPStore{
		accounts: map[string]*auth.Account{},
		byEmail:  map[string]*auth.Account{},
		pending:  map[string][]byte{},
		lastStep: map[string]int64{},
		recovery: map[string]map[string]bool{},
	}
}

func (s *domainTOTPStore) seed(t *testing.T, id, email, pw, role string) *auth.Account {
	t.Helper()
	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	a := &auth.Account{ID: id, Email: email, PasswordHash: hash, Role: role, Active: true}
	s.accounts[id] = a
	s.byEmail[email] = a
	return a
}

func (s *domainTOTPStore) GetByEmail(_ context.Context, email string) (*auth.Account, error) {
	a, ok := s.byEmail[email]
	if !ok {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *domainTOTPStore) GetByID(_ context.Context, id string) (*auth.Account, error) {
	a, ok := s.accounts[id]
	if !ok {
		return nil, auth.ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (s *domainTOTPStore) UpdateLastLogin(context.Context, string) error { return nil }

func (s *domainTOTPStore) UpdatePasswordHash(context.Context, string, string) error { return nil }

func (s *domainTOTPStore) CreateAccount(context.Context, string, string, string, string) (string, bool, error) {
	return "", false, nil
}

func (s *domainTOTPStore) SetPendingTOTPSecret(_ context.Context, id string, encrypted []byte) error {
	a, ok := s.accounts[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	s.pending[id] = encrypted
	a.TOTPEnabled = false
	return nil
}

func (s *domainTOTPStore) ConfirmTOTPEnrollment(_ context.Context, id string) error {
	a, ok := s.accounts[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	a.TOTPEnabled = true
	return nil
}

func (s *domainTOTPStore) ConfirmTOTPEnrollmentWithRecoveryCodes(_ context.Context, id string, hashedCodes [][]byte) error {
	a, ok := s.accounts[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	a.TOTPEnabled = true
	m := map[string]bool{}
	for _, h := range hashedCodes {
		m[string(h)] = true
	}
	s.recovery[id] = m
	return nil
}

func (s *domainTOTPStore) GetTOTPSecret(_ context.Context, id string) ([]byte, error) {
	if _, ok := s.accounts[id]; !ok {
		return nil, auth.ErrAccountNotFound
	}
	enc, ok := s.pending[id]
	if !ok {
		return nil, auth.ErrTOTPNotEnrolled
	}
	return enc, nil
}

func (s *domainTOTPStore) DisableTOTP(_ context.Context, id string) error {
	a, ok := s.accounts[id]
	if !ok {
		return auth.ErrAccountNotFound
	}
	delete(s.pending, id)
	delete(s.lastStep, id)
	delete(s.recovery, id)
	a.TOTPEnabled = false
	return nil
}

func (s *domainTOTPStore) ConsumeTOTPStep(_ context.Context, id string, step int64) (bool, error) {
	last, has := s.lastStep[id]
	if has && step <= last {
		return false, nil
	}
	s.lastStep[id] = step
	return true, nil
}

func (s *domainTOTPStore) StoreRecoveryCodes(_ context.Context, id string, hashes [][]byte) error {
	m := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		m[string(h)] = false
	}
	s.recovery[id] = m
	return nil
}

func (s *domainTOTPStore) ConsumeRecoveryCode(_ context.Context, id string, hash []byte) (bool, error) {
	m, ok := s.recovery[id]
	if !ok {
		return false, nil
	}
	used, exists := m[string(hash)]
	if !exists || used {
		return false, nil
	}
	m[string(hash)] = true
	return true, nil
}

var (
	_ auth.AccountStore = (*domainTOTPStore)(nil)
	_ auth.TOTPStore    = (*domainTOTPStore)(nil)
)

// testEncKey is an exact-length (auth.TOTPEncryptionKeyLen) fixture key,
// built via bytes.Repeat rather than a hand-counted literal so its length is
// correct by construction, not by manual character-counting.
var testEncKey = bytes.Repeat([]byte("k"), auth.TOTPEncryptionKeyLen)

// ── StartTOTPEnrollment ──────────────────────────────────────────────────

func TestStartTOTPEnrollment_MintsOnceThenRebuildsOnReload(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	ctx := context.Background()

	secret1, uri1, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment (mint): %v", err)
	}
	if secret1 == "" || uri1 == "" {
		t.Fatal("StartTOTPEnrollment (mint): expected non-empty secret and URI")
	}
	if _, err := store.GetTOTPSecret(ctx, acct.ID); err != nil {
		t.Fatalf("expected a pending secret to be persisted after mint: %v", err)
	}

	// Reload: must rebuild the SAME secret, never mint a second one.
	secret2, uri2, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment (reload): %v", err)
	}
	if secret2 != secret1 {
		t.Fatalf("reload minted a DIFFERENT secret: got %q, want %q (orphans the first)", secret2, secret1)
	}
	if uri2 != uri1 {
		t.Fatalf("reload produced a different otpauth URI: got %q, want %q", uri2, uri1)
	}
}

func TestStartTOTPEnrollment_AlreadyEnabled_RefusesAndDoesNotTouchStore(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	acct.TOTPEnabled = true
	ctx := context.Background()

	_, _, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct)
	if !errors.Is(err, auth.ErrTOTPAlreadyEnabled) {
		t.Fatalf("expected ErrTOTPAlreadyEnabled, got %v", err)
	}
	if _, ok := store.pending[acct.ID]; ok {
		t.Fatal("StartTOTPEnrollment must not write a pending secret when already enabled")
	}
}

// ── BuildTOTPQRPNG ───────────────────────────────────────────────────────

// TestBuildTOTPQRPNG_EncodesTheStoredSecret falsifies that the served QR
// actually encodes the SAME secret StartTOTPEnrollment minted -- not just
// "some valid PNG". It reconstructs an independent reference PNG straight
// from the known-correct secret (auth.RebuildTOTPKey + auth.GenerateQRPNG,
// bypassing the store entirely) and asserts byte-for-byte equality against
// BuildTOTPQRPNG's output: any bug in the encrypt/store/decrypt round trip,
// or in which account's secret gets read, changes the encoded otpauth URI
// and therefore every pixel of the QR bitmap.
func TestBuildTOTPQRPNG_EncodesTheStoredSecret(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	ctx := context.Background()
	const issuer = "go-panel-test"
	const size = 256

	secret, _, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, issuer, acct)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment: %v", err)
	}

	got, err := auth.BuildTOTPQRPNG(ctx, store, testEncKey, issuer, acct, size, size)
	if err != nil {
		t.Fatalf("BuildTOTPQRPNG: %v", err)
	}

	wantKey, err := auth.RebuildTOTPKey(issuer, acct.Email, secret)
	if err != nil {
		t.Fatalf("RebuildTOTPKey (reference): %v", err)
	}
	want, err := auth.GenerateQRPNG(wantKey, size, size)
	if err != nil {
		t.Fatalf("GenerateQRPNG (reference): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("BuildTOTPQRPNG did not encode the same secret StartTOTPEnrollment minted (PNG bytes differ from the independently reconstructed reference)")
	}

	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decoding BuildTOTPQRPNG output as PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
		t.Fatalf("QR image is %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
	}
}

func TestBuildTOTPQRPNG_NotEnrolled(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")

	_, err := auth.BuildTOTPQRPNG(context.Background(), store, testEncKey, "go-panel-test", acct, 256, 256)
	if !errors.Is(err, auth.ErrTOTPNotEnrolled) {
		t.Fatalf("expected ErrTOTPNotEnrolled, got %v", err)
	}
}

// ── ConfirmTOTPEnrollment ────────────────────────────────────────────────

func validCodeFor(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, at) // independent oracle, same as totp_test.go
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	return code
}

func TestConfirmTOTPEnrollment_CorrectCode_EnablesAndReturnsCodes(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	ctx := context.Background()
	now := time.Now()

	secret, _, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment: %v", err)
	}
	code := validCodeFor(t, secret, now)

	codes, err := auth.ConfirmTOTPEnrollment(ctx, store, testEncKey, acct, code, now)
	if err != nil {
		t.Fatalf("ConfirmTOTPEnrollment: %v", err)
	}
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), auth.RecoveryCodeCount)
	}
	got, err := store.GetByID(ctx, acct.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.TOTPEnabled {
		t.Fatal("TOTPEnabled must be true after a successful ConfirmTOTPEnrollment")
	}
}

func TestConfirmTOTPEnrollment_WrongCode_StaysDisabledNoRecoveryCodes(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	ctx := context.Background()
	now := time.Now()

	if _, _, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct); err != nil {
		t.Fatalf("StartTOTPEnrollment: %v", err)
	}

	_, err := auth.ConfirmTOTPEnrollment(ctx, store, testEncKey, acct, "000000", now)
	if !errors.Is(err, auth.ErrTOTPCodeInvalid) {
		t.Fatalf("expected ErrTOTPCodeInvalid, got %v", err)
	}
	if got, _ := store.GetByID(ctx, acct.ID); got.TOTPEnabled {
		t.Fatal("TOTPEnabled must stay false after a wrong code")
	}
	if len(store.recovery[acct.ID]) != 0 {
		t.Fatal("no recovery codes must be generated after a wrong code")
	}
}

// TestConfirmTOTPEnrollment_ReplayedCode_SecondAttemptRejected is the
// first live exercise of P3's replay guard (ConsumeTOTPStep) wired to this
// phase's confirm flow: the SAME code, submitted twice at the SAME instant,
// must succeed once and be rejected the second time even though it is
// still cryptographically valid at that instant.
func TestConfirmTOTPEnrollment_ReplayedCode_SecondAttemptRejected(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	ctx := context.Background()
	now := time.Now()

	secret, _, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment: %v", err)
	}
	code := validCodeFor(t, secret, now)

	if _, err := auth.ConfirmTOTPEnrollment(ctx, store, testEncKey, acct, code, now); err != nil {
		t.Fatalf("first ConfirmTOTPEnrollment: expected success, got %v", err)
	}

	// acct is now enrolled -- a second Confirm must ALSO refuse via
	// ErrTOTPAlreadyEnabled before ever re-checking the code. Reset the
	// local copy's flag to false to isolate the replay-guard assertion
	// specifically (proves ConsumeTOTPStep itself rejects the replay,
	// independent of the already-enabled guard added alongside it).
	reenroll := *acct
	reenroll.TOTPEnabled = false
	if _, err := auth.ConfirmTOTPEnrollment(ctx, store, testEncKey, &reenroll, code, now); !errors.Is(err, auth.ErrTOTPCodeInvalid) {
		t.Fatalf("replayed code (already-enabled guard bypassed): expected ErrTOTPCodeInvalid from the replay guard, got %v", err)
	}
}

func TestConfirmTOTPEnrollment_AlreadyEnabled_RefusesWithoutRotatingRecoveryCodes(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "pw", "admin")
	ctx := context.Background()
	now := time.Now()

	secret, _, err := auth.StartTOTPEnrollment(ctx, store, testEncKey, "go-panel-test", acct)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment: %v", err)
	}
	code := validCodeFor(t, secret, now)
	if _, err := auth.ConfirmTOTPEnrollment(ctx, store, testEncKey, acct, code, now); err != nil {
		t.Fatalf("initial ConfirmTOTPEnrollment: %v", err)
	}
	originalCodes := make(map[string]bool, len(store.recovery[acct.ID]))
	for h, used := range store.recovery[acct.ID] {
		originalCodes[h] = used
	}

	// acct.TOTPEnabled is now true (store.seed returned a pointer the
	// fake store mutates in place). A second Confirm attempt -- e.g. an
	// operator's browser re-POSTing a stale form, or a hijacked session
	// replaying a live code that is NOT the password -- must refuse
	// without touching the recovery-code set already on file.
	laterCode := validCodeFor(t, secret, now.Add(30*time.Second))
	_, err = auth.ConfirmTOTPEnrollment(ctx, store, testEncKey, acct, laterCode, now.Add(30*time.Second))
	if !errors.Is(err, auth.ErrTOTPAlreadyEnabled) {
		t.Fatalf("expected ErrTOTPAlreadyEnabled on an already-enrolled account, got %v", err)
	}
	if len(store.recovery[acct.ID]) != len(originalCodes) {
		t.Fatal("recovery-code set changed size on a refused re-confirm")
	}
	for h := range originalCodes {
		if _, stillPresent := store.recovery[acct.ID][h]; !stillPresent {
			t.Fatal("an original recovery code hash was removed by a refused re-confirm")
		}
	}
}

// ── DisableTOTPWithReauth / RegenerateRecoveryCodesWithReauth ───────────

func TestDisableTOTPWithReauth_WrongPassword_StaysEnabled(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "correct-pw", "admin")
	ctx := context.Background()
	acct.TOTPEnabled = true // simulate an already-confirmed enrollment

	err := auth.DisableTOTPWithReauth(ctx, store, store, acct, "wrong-pw")
	if !errors.Is(err, auth.ErrReauthFailed) {
		t.Fatalf("expected ErrReauthFailed, got %v", err)
	}
	if got, _ := store.GetByID(ctx, acct.ID); !got.TOTPEnabled {
		t.Fatal("TOTPEnabled must stay true after a failed re-auth")
	}
}

func TestDisableTOTPWithReauth_CorrectPassword_Disables(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "correct-pw", "admin")
	ctx := context.Background()
	acct.TOTPEnabled = true
	store.pending[acct.ID] = []byte("some-encrypted-secret")
	store.recovery[acct.ID] = map[string]bool{"h1": false}

	if err := auth.DisableTOTPWithReauth(ctx, store, store, acct, "correct-pw"); err != nil {
		t.Fatalf("DisableTOTPWithReauth: %v", err)
	}
	got, _ := store.GetByID(ctx, acct.ID)
	if got.TOTPEnabled {
		t.Fatal("TOTPEnabled must be false after a correctly re-authenticated disable")
	}
	if _, ok := store.pending[acct.ID]; ok {
		t.Fatal("the secret must be cleared on disable")
	}
	if len(store.recovery[acct.ID]) != 0 {
		t.Fatal("recovery codes must be cleared on disable")
	}
}

func TestRegenerateRecoveryCodesWithReauth_WrongPassword_KeepsOldCodes(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "correct-pw", "admin")
	acct.TOTPEnabled = true
	ctx := context.Background()
	store.recovery[acct.ID] = map[string]bool{"original-hash": false}

	_, err := auth.RegenerateRecoveryCodesWithReauth(ctx, store, store, acct, "wrong-pw")
	if !errors.Is(err, auth.ErrReauthFailed) {
		t.Fatalf("expected ErrReauthFailed, got %v", err)
	}
	if _, ok := store.recovery[acct.ID]["original-hash"]; !ok {
		t.Fatal("the original recovery-code set must survive a failed re-auth")
	}
}

func TestRegenerateRecoveryCodesWithReauth_CorrectPassword_ReplacesCodes(t *testing.T) {
	store := newDomainTOTPStore()
	acct := store.seed(t, "u1", "op@example.com", "correct-pw", "admin")
	acct.TOTPEnabled = true
	ctx := context.Background()
	store.recovery[acct.ID] = map[string]bool{"original-hash": false}

	codes, err := auth.RegenerateRecoveryCodesWithReauth(ctx, store, store, acct, "correct-pw")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodesWithReauth: %v", err)
	}
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), auth.RecoveryCodeCount)
	}
	if _, stillThere := store.recovery[acct.ID]["original-hash"]; stillThere {
		t.Fatal("the old recovery-code set must be replaced, not merged")
	}
}

// ── VerifyAccountPassword ────────────────────────────────────────────────

func TestVerifyAccountPassword(t *testing.T) {
	store := newDomainTOTPStore()
	store.seed(t, "u1", "op@example.com", "correct-pw", "admin")
	ctx := context.Background()

	if !auth.VerifyAccountPassword(ctx, store, "op@example.com", "correct-pw") {
		t.Error("expected the correct password to verify")
	}
	if auth.VerifyAccountPassword(ctx, store, "op@example.com", "wrong-pw") {
		t.Error("expected a wrong password to fail")
	}
	if auth.VerifyAccountPassword(ctx, store, "nobody@example.com", "x") {
		t.Error("expected an unknown email to fail, not panic or error out")
	}
}
