package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
	"unicode"
)

// recoveryCodeBytes is the per-code raw random length: 16 bytes = 128 bits,
// meeting the spec's >=128-bit entropy floor exactly (no over-provisioning
// beyond what was asked). 128 bits is computationally infeasible to
// brute-force offline even against the fast SHA-256 hash used for storage
// (see HashRecoveryCode).
const recoveryCodeBytes = 16

// RecoveryCodeCount is the recommended number of single-use recovery codes
// to mint per enrollment (GenerateRecoveryCodes(RecoveryCodeCount)),
// matching the council-vetted spec. Exported so a future enrollment
// handler (P4) has one canonical value to call, rather than a
// re-guessed literal.
const RecoveryCodeCount = 10

// recoveryCodeGroupSize dash-groups the encoded code for readability
// (copy/paste, manual transcription). The final group may be shorter than
// this when the encoded length isn't an exact multiple of it.
const recoveryCodeGroupSize = 4

// crockfordAlphabet is Crockford's Base32 (https://www.crockford.com/base32.html):
// the standard RFC 4648 base32 alphabet with I, L, O, U removed to
// eliminate visual ambiguity with 1/1/0/V. This is the stdlib base32
// ALGORITHM with a swapped alphabet table — not a hand-rolled encoding —
// used here purely for human-readable recovery codes. The TOTP shared
// secret itself (totp.go) stays standard RFC 4648 base32, as required by
// authenticator apps; these are two independent encodings for two
// independent purposes.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var recoveryCodeEncoding = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)

// GenerateRecoveryCodes mints n single-use TOTP recovery codes, each backed
// by recoveryCodeBytes (128 bits) of crypto/rand entropy and rendered as a
// dash-grouped Crockford base32 string for display. hashes[i] is exactly
// HashRecoveryCode(codes[i]) — the SHA-256 digest a caller persists via
// TOTPStore.StoreRecoveryCodes. Codes are meant to be shown to the operator
// exactly once; callers must not store the plaintext codes.
func GenerateRecoveryCodes(n int) (codes []string, hashes [][]byte, err error) {
	if n <= 0 {
		return nil, nil, fmt.Errorf("auth: GenerateRecoveryCodes: n must be positive, got %d", n)
	}
	codes = make([]string, n)
	hashes = make([][]byte, n)
	for i := range n {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("auth: generate recovery code: %w", err)
		}
		code := groupCode(recoveryCodeEncoding.EncodeToString(raw), recoveryCodeGroupSize)
		codes[i] = code
		hashes[i] = HashRecoveryCode(code)
	}
	return codes, hashes, nil
}

// groupCode inserts "-" every groupSize runes for readability. The final
// group may be shorter than groupSize when len(s) isn't an exact multiple.
func groupCode(s string, groupSize int) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%groupSize == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeRecoveryCode uppercases and strips dashes/whitespace so a
// user-submitted code hashes identically regardless of casing or how they
// copy/typed the dash grouping.
func normalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// HashRecoveryCode normalizes code (case/whitespace/dash-insensitive, see
// normalizeRecoveryCode) and returns its SHA-256 digest — the form stored
// by TOTPStore.StoreRecoveryCodes and compared by ConsumeRecoveryCode. A
// fast hash is the right tool at >=128 bits of input entropy: even an
// offline DB-dump attacker computing SHA-256 at billions/sec cannot
// brute-force a 128-bit space, and bcrypt/scrypt's deliberate slowness
// (designed to slow down brute-forcing LOW-entropy human passwords) would
// only add CPU cost here with no corresponding security benefit.
func HashRecoveryCode(code string) []byte {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return sum[:]
}
