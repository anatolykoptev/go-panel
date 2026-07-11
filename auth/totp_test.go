package auth_test

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/anatolykoptev/go-panel/auth"
	"github.com/pquerna/otp/totp"
)

// TestValidateTOTPCodeAt_RFC6238KnownAnswer pins correctness against a
// published RFC vector, independent of auth.GenerateTOTPSecret (a pure
// known-answer test, not a round-trip).
//
// Secret: ASCII "12345678901234567890" (the RFC 4226 Appendix D / RFC 6238
// Appendix B seed for SHA1), base32-encoded as
// "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" (RFC 6238 Appendix B's own example).
// At T=59s (period 30 -> counter=1), RFC 6238 Appendix B's 8-digit vector
// is "94287082"; RFC 4226 Appendix D's counter=1 6-digit HOTP vector is
// independently published as "287082" -- and 94287082 mod 1e6 == 287082,
// so the two RFCs agree. auth.ValidateTOTPCode always requests 6 digits
// (see totp.go), so "287082" is the expected code here.
func TestValidateTOTPCodeAt_RFC6238KnownAnswer(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	const wantCode = "287082"
	at := time.Unix(59, 0).UTC()

	if !auth.ValidateTOTPCodeAt(secret, wantCode, 0, at) {
		t.Fatalf("RFC known-answer code %q did not validate at T=59s, skew=0", wantCode)
	}
	if auth.ValidateTOTPCodeAt(secret, "000000", 0, at) {
		t.Fatal("an arbitrary wrong code validated against the RFC vector")
	}
}

func TestGenerateAndValidateTOTPCode_RoundTrip(t *testing.T) {
	key, err := auth.GenerateTOTPSecret("go-panel-test", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	secret := key.Secret()
	if secret == "" {
		t.Fatal("GenerateTOTPSecret returned an empty Secret()")
	}

	now := time.Now()
	code, err := totp.GenerateCode(secret, now) // library used as an independent oracle
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}

	if !auth.ValidateTOTPCodeAt(secret, code, auth.DefaultTOTPSkew, now) {
		t.Fatal("a freshly generated correct code failed to validate")
	}

	wrong := "0" + code[1:]
	if wrong == code {
		wrong = "1" + code[1:]
	}
	if auth.ValidateTOTPCodeAt(secret, wrong, auth.DefaultTOTPSkew, now) {
		t.Fatal("a mutated (wrong) code validated")
	}
}

func TestValidateTOTPCodeAt_SkewWindow(t *testing.T) {
	key, err := auth.GenerateTOTPSecret("go-panel-test", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	secret := key.Secret()
	now := time.Unix(1_700_000_100, 0).UTC()

	codeAt := func(offsetSteps int) string {
		t.Helper()
		when := now.Add(time.Duration(offsetSteps) * 30 * time.Second)
		code, err := totp.GenerateCode(secret, when)
		if err != nil {
			t.Fatalf("totp.GenerateCode offset %d: %v", offsetSteps, err)
		}
		return code
	}

	oneAhead, oneBehind := codeAt(1), codeAt(-1)
	twoAhead, twoBehind := codeAt(2), codeAt(-2)

	const skew1 = 1
	if !auth.ValidateTOTPCodeAt(secret, oneAhead, skew1, now) {
		t.Error("code from +1 step must validate at skew=1")
	}
	if !auth.ValidateTOTPCodeAt(secret, oneBehind, skew1, now) {
		t.Error("code from -1 step must validate at skew=1")
	}
	if auth.ValidateTOTPCodeAt(secret, twoAhead, skew1, now) {
		t.Error("code from +2 steps must NOT validate at skew=1")
	}
	if auth.ValidateTOTPCodeAt(secret, twoBehind, skew1, now) {
		t.Error("code from -2 steps must NOT validate at skew=1")
	}

	const skew2 = 2
	if !auth.ValidateTOTPCodeAt(secret, twoAhead, skew2, now) {
		t.Error("code from +2 steps must validate at skew=2")
	}
	if !auth.ValidateTOTPCodeAt(secret, twoBehind, skew2, now) {
		t.Error("code from -2 steps must validate at skew=2")
	}
}

func TestValidateTOTPCode_MalformedInputFailsClosed(t *testing.T) {
	cases := []struct{ secret, code string }{
		{"", "123456"},
		{"GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", ""},
		{"not-valid-base32!!", "123456"},
		{"GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "not-digits"},
	}
	for _, c := range cases {
		if auth.ValidateTOTPCode(c.secret, c.code, auth.DefaultTOTPSkew) {
			t.Errorf("ValidateTOTPCode(%q, %q, _) = true, want false (fail closed)", c.secret, c.code)
		}
	}
}

func TestGenerateTOTPSecret_RequiresIssuerAndAccountName(t *testing.T) {
	if _, err := auth.GenerateTOTPSecret("", "user@example.com"); err == nil {
		t.Error("GenerateTOTPSecret with empty issuer: expected error, got nil")
	}
	if _, err := auth.GenerateTOTPSecret("go-panel-test", ""); err == nil {
		t.Error("GenerateTOTPSecret with empty accountName: expected error, got nil")
	}
}

func TestGenerateQRPNG_ProducesDecodablePNGAtRequestedSize(t *testing.T) {
	key, err := auth.GenerateTOTPSecret("go-panel-test", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	const size = 200
	data, err := auth.GenerateQRPNG(key, size, size)
	if err != nil {
		t.Fatalf("GenerateQRPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding GenerateQRPNG output as PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != size || b.Dy() != size {
		t.Fatalf("QR image is %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
	}
}

func TestRebuildTOTPKey_FunctionallyMatchesOriginal(t *testing.T) {
	original, err := auth.GenerateTOTPSecret("go-panel-test", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	secret := original.Secret()

	rebuilt, err := auth.RebuildTOTPKey("go-panel-test", "user@example.com", secret)
	if err != nil {
		t.Fatalf("RebuildTOTPKey: %v", err)
	}
	if rebuilt.Secret() != secret {
		t.Fatalf("RebuildTOTPKey secret = %q, want %q", rebuilt.Secret(), secret)
	}

	// Functional check, not just string equality: a code generated against
	// the ORIGINAL key's secret must validate against the REBUILT key's
	// secret, proving RebuildTOTPKey reconstructs a usable, equivalent key.
	now := time.Now()
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	if !auth.ValidateTOTPCodeAt(rebuilt.Secret(), code, auth.DefaultTOTPSkew, now) {
		t.Fatal("a code valid for the original secret did not validate against the rebuilt key's secret")
	}
}

func TestTOTPStepAt_MatchesRFCPeriod(t *testing.T) {
	// T=59s falls in period [30,60) -> step 1, per RFC 6238's own worked
	// example (see TestValidateTOTPCodeAt_RFC6238KnownAnswer).
	if got := auth.TOTPStepAt(time.Unix(59, 0).UTC()); got != 1 {
		t.Errorf("TOTPStepAt(59s) = %d, want 1", got)
	}
	if got := auth.TOTPStepAt(time.Unix(29, 0).UTC()); got != 0 {
		t.Errorf("TOTPStepAt(29s) = %d, want 0", got)
	}
	if got := auth.TOTPStepAt(time.Unix(60, 0).UTC()); got != 2 {
		t.Errorf("TOTPStepAt(60s) = %d, want 2", got)
	}
}
