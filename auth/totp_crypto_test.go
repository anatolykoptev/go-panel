package auth_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/anatolykoptev/go-panel/auth"
)

func mustRandBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return b
}

func TestEncryptDecryptTOTPSecret_RoundTrip(t *testing.T) {
	key := mustRandBytes(t, auth.TOTPEncryptionKeyLen)
	plaintext := []byte("JBSWY3DPEHPK3PXP") // a base32 TOTP secret, as auth.GenerateTOTPSecret produces

	ct, err := auth.EncryptTOTPSecret(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret: %v", err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	pt, err := auth.DecryptTOTPSecret(ct, key)
	if err != nil {
		t.Fatalf("DecryptTOTPSecret: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("round-trip mismatch: got %q, want %q", pt, plaintext)
	}
}

func TestEncryptTOTPSecret_NonceRandomizedPerCall(t *testing.T) {
	key := mustRandBytes(t, auth.TOTPEncryptionKeyLen)
	plaintext := []byte("same-plaintext-both-times")

	ct1, err := auth.EncryptTOTPSecret(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret (1): %v", err)
	}
	ct2, err := auth.EncryptTOTPSecret(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret (2): %v", err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext -- nonce reuse")
	}
}

func TestDecryptTOTPSecret_TamperedCiphertextFailsClosed(t *testing.T) {
	key := mustRandBytes(t, auth.TOTPEncryptionKeyLen)
	ct, err := auth.EncryptTOTPSecret([]byte("a-totp-secret"), key)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret: %v", err)
	}
	tampered := append([]byte(nil), ct...)
	tampered[len(tampered)-1] ^= 0xFF // flip the last ciphertext byte

	if _, err := auth.DecryptTOTPSecret(tampered, key); err == nil {
		t.Fatal("DecryptTOTPSecret accepted a tampered ciphertext")
	}
}

func TestDecryptTOTPSecret_WrongKeyFailsClosed(t *testing.T) {
	key := mustRandBytes(t, auth.TOTPEncryptionKeyLen)
	wrongKey := mustRandBytes(t, auth.TOTPEncryptionKeyLen)
	ct, err := auth.EncryptTOTPSecret([]byte("a-totp-secret"), key)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret: %v", err)
	}
	if _, err := auth.DecryptTOTPSecret(ct, wrongKey); err == nil {
		t.Fatal("DecryptTOTPSecret accepted the wrong key")
	}
}

func TestEncryptDecryptTOTPSecret_WrongKeyLengthReturnsError(t *testing.T) {
	plaintext := []byte("a-totp-secret")
	cases := map[string][]byte{
		"short(16)": mustRandBytes(t, 16), // AES-128 size, not the required 32
		"long(64)":  mustRandBytes(t, 64),
		"nil":       nil,
	}
	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := auth.EncryptTOTPSecret(plaintext, k); err == nil {
				t.Fatalf("EncryptTOTPSecret with %s key: expected error, got nil", name)
			}
			if _, err := auth.DecryptTOTPSecret(plaintext, k); err == nil {
				t.Fatalf("DecryptTOTPSecret with %s key: expected error, got nil", name)
			}
		})
	}
}

func TestDecryptTOTPSecret_TruncatedCiphertextFailsClosed(t *testing.T) {
	key := mustRandBytes(t, auth.TOTPEncryptionKeyLen)
	if _, err := auth.DecryptTOTPSecret([]byte("short"), key); err == nil {
		t.Fatal("DecryptTOTPSecret accepted ciphertext shorter than the nonce")
	}
}
