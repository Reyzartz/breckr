package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
This package is the only thing standing between a leaked database file and a
leaked bot token, so its failure modes matter more than its happy path: a
tampered value must be rejected rather than half-decrypted, and a key file the
rest of the machine can read must be refused rather than quietly used.
*/

func newCipher(t *testing.T) *Cipher {
	t.Helper()

	key, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}

	cipher, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return cipher
}

func TestARoundTripReturnsTheOriginal(t *testing.T) {
	cipher := newCipher(t)

	plaintext := `{"token":"123:abc","chat_id":"42"}`

	encrypted, err := cipher.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(encrypted, "123:abc") {
		t.Fatal("the ciphertext still contains the secret in the clear")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Fatalf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

// A fresh nonce per call: identical configs must not produce identical rows, or
// the database leaks which channels share a credential.
func TestEncryptingTwiceProducesDifferentCiphertext(t *testing.T) {
	cipher := newCipher(t)

	first, err := cipher.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := cipher.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if first == second {
		t.Fatal("the same plaintext encrypted to the same ciphertext -- the nonce is not fresh")
	}
}

// GCM authenticates, so a modified row fails loudly instead of decrypting to
// plausible garbage that would then be sent somewhere.
func TestATamperedValueIsRejected(t *testing.T) {
	cipher := newCipher(t)

	encrypted, err := cipher.Encrypt([]byte(`{"token":"real"}`))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Flip the final character to something else in the base64 alphabet.
	tampered := encrypted[:len(encrypted)-1]
	if strings.HasSuffix(encrypted, "A") {
		tampered += "B"
	} else {
		tampered += "A"
	}

	if _, err := cipher.Decrypt(tampered); err == nil {
		t.Fatal("a tampered ciphertext must not decrypt")
	}
}

func TestTheWrongKeyCannotDecrypt(t *testing.T) {
	first := newCipher(t)
	second := newCipher(t)

	encrypted, err := first.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := second.Decrypt(encrypted); err == nil {
		t.Fatal("a different key must not decrypt the value")
	}
}

func TestANewKeyFileIsCreatedPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if len(key) != KeySize {
		t.Fatalf("key is %d bytes, want %d", len(key), KeySize)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %04o, want 0600 -- a key others can read is not a key", perm)
	}
}

// Loading is what happens on every boot after the first, so it has to return the
// same key rather than rotating and orphaning every stored channel.
func TestLoadingIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	first, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	second, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}

	if string(first) != string(second) {
		t.Fatal("the key changed between loads -- every stored channel would be unreadable")
	}
}

// Refused rather than silently tightened, so the operator learns how it got
// loosened -- usually a `chmod -R` or a volume mount.
func TestAWorldReadableKeyFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, err := LoadOrCreateKey(path)
	if err == nil {
		t.Fatal("a key file readable by other users must be refused")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("error = %q, want it to say how to fix it", err)
	}
}

// A truncated key would otherwise fail later as an opaque decrypt error on every
// channel at once.
func TestATruncatedKeyFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	if err := os.WriteFile(path, []byte("too short"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := LoadOrCreateKey(path); err == nil {
		t.Fatal("a key of the wrong length must be refused")
	}
}
