// Package crypto encrypts the channel credentials before they reach SQLite.
//
// Channels hold bot tokens, webhook URLs and app passwords -- things that used
// to live only in .env, where a leaked database file gave nothing away. Now that
// they are rows, the database file alone must stay useless without the key
// beside it.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeySize is the AES-256 key length, in bytes.
const KeySize = 32

// keyFileMode is what a fresh key file is written with, and the most permissive
// mode an existing one may carry.
const keyFileMode os.FileMode = 0o600

type Cipher struct {
	aead cipher.AEAD
}

// LoadOrCreateKey reads the master key, generating one on first boot.
//
// Generating rather than requiring an env var is deliberate: the key is machine
// state, not configuration, and a setup step that can be forgotten is a setup
// step that eventually ships an unencrypted database.
func LoadOrCreateKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	switch {
	case err == nil:
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("could not stat the key file at %s: %w", path, statErr)
		}
		// A key anyone on the box can read is not a key. Refuse rather than
		// silently tightening it, so the operator learns how it got loosened.
		if info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf(
				"the key file at %s is readable by other users (mode %04o). Run: chmod 600 %s",
				path, info.Mode().Perm(), path,
			)
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf(
				"the key file at %s is %d bytes, expected %d. Restore the original file -- channel credentials cannot be decrypted without it",
				path, len(key), KeySize,
			)
		}
		return key, nil

	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("could not create the key directory: %w", err)
		}

		key := make([]byte, KeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("could not generate an encryption key: %w", err)
		}
		if err := os.WriteFile(path, key, keyFileMode); err != nil {
			return nil, fmt.Errorf("could not write the key file to %s: %w", path, err)
		}
		return key, nil

	default:
		return nil, fmt.Errorf("could not read the key file at %s: %w", path, err)
	}
}

func New(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("could not build the cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("could not build the cipher: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

// Encrypt returns base64(nonce ‖ ciphertext), which is safe to put in a TEXT
// column.
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("could not generate a nonce: %w", err)
	}

	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. GCM authenticates, so a tampered or wrong-key value
// fails here rather than returning plausible garbage.
func (c *Cipher) Decrypt(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("stored value is not valid base64: %w", err)
	}

	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return nil, errors.New("stored value is too short to be encrypted data")
	}

	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt -- the key file may have changed: %w", err)
	}
	return plaintext, nil
}
