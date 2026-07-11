package auth_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
)

func TestGenerateRecoveryCodes_CountAndSelfConsistentHashes(t *testing.T) {
	codes, hashes, err := auth.GenerateRecoveryCodes(auth.RecoveryCodeCount)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != auth.RecoveryCodeCount || len(hashes) != auth.RecoveryCodeCount {
		t.Fatalf("got %d codes / %d hashes, want %d each", len(codes), len(hashes), auth.RecoveryCodeCount)
	}
	for i, code := range codes {
		want := auth.HashRecoveryCode(code)
		if !bytes.Equal(hashes[i], want) {
			t.Errorf("hashes[%d] does not match HashRecoveryCode(codes[%d]) -- generate and verify must hash identically", i, i)
		}
	}
}

func TestGenerateRecoveryCodes_AllDistinct(t *testing.T) {
	codes, hashes, err := auth.GenerateRecoveryCodes(auth.RecoveryCodeCount)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	seenCodes := make(map[string]bool, len(codes))
	for _, c := range codes {
		if seenCodes[c] {
			t.Fatalf("duplicate recovery code generated: %q", c)
		}
		seenCodes[c] = true
	}
	seenHashes := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		k := string(h)
		if seenHashes[k] {
			t.Fatalf("duplicate recovery code hash generated: %x", h)
		}
		seenHashes[k] = true
	}
}

func TestGenerateRecoveryCodes_MeetsEntropyFloor(t *testing.T) {
	codes, _, err := auth.GenerateRecoveryCodes(1)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	// 16 raw bytes = 128 bits, the spec's entropy floor; base32 needs
	// ceil(128/5) = 26 characters to represent that many bits. If the
	// underlying byte count regresses below 16, this length shrinks too.
	const wantChars = 26
	stripped := strings.ReplaceAll(codes[0], "-", "")
	if len(stripped) != wantChars {
		t.Fatalf("recovery code %q has %d non-dash chars, want %d (128-bit floor)", codes[0], len(stripped), wantChars)
	}
}

func TestGenerateRecoveryCodes_CrockfordAlphabetExcludesAmbiguousChars(t *testing.T) {
	codes, _, err := auth.GenerateRecoveryCodes(20) // enough draws to make each excluded letter likely if present
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	for _, c := range codes {
		for _, ambiguous := range []rune{'I', 'L', 'O', 'U'} {
			if strings.ContainsRune(c, ambiguous) {
				t.Fatalf("recovery code %q contains ambiguous Crockford-excluded char %q", c, ambiguous)
			}
		}
	}
}

func TestGenerateRecoveryCodes_GroupedForReadability(t *testing.T) {
	codes, _, err := auth.GenerateRecoveryCodes(1)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if !strings.Contains(codes[0], "-") {
		t.Fatalf("recovery code %q is not dash-grouped", codes[0])
	}
}

func TestGenerateRecoveryCodes_RejectsNonPositiveN(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, _, err := auth.GenerateRecoveryCodes(n); err == nil {
			t.Errorf("GenerateRecoveryCodes(%d): expected error, got nil", n)
		}
	}
}

func TestHashRecoveryCode_NormalizesCaseWhitespaceAndDashes(t *testing.T) {
	codes, hashes, err := auth.GenerateRecoveryCodes(1)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	original := codes[0]
	wantHash := hashes[0]

	variants := []string{
		strings.ToLower(original),
		strings.ReplaceAll(original, "-", ""),
		" " + original + " ",
		strings.ReplaceAll(strings.ToLower(original), "-", "  "),
	}
	for _, v := range variants {
		if got := auth.HashRecoveryCode(v); !bytes.Equal(got, wantHash) {
			t.Errorf("HashRecoveryCode(%q) = %x, want %x (must normalize to match the canonical form)", v, got, wantHash)
		}
	}
}

func TestHashRecoveryCode_DifferentCodesHashDifferently(t *testing.T) {
	h1 := auth.HashRecoveryCode("AAAA-AAAA-AAAA-AAAA-AAAA-AAAA-AA")
	h2 := auth.HashRecoveryCode("BBBB-BBBB-BBBB-BBBB-BBBB-BBBB-BB")
	if bytes.Equal(h1, h2) {
		t.Fatal("distinct recovery codes hashed to the same digest")
	}
}
