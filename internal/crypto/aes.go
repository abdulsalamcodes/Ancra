// Package crypto provides application-level AES-256-GCM encryption for
// sensitive values stored in the database (e.g. per-org Nomba credentials).
//
// Format: base64( nonce || ciphertext )
// The 12-byte nonce is randomly generated per encryption and prepended to the
// ciphertext so that the same plaintext produces different output each time.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	keyLength   = 32 // AES-256
	nonceLength = 12 // GCM standard nonce size
)

// ErrInvalidKey is returned when the encryption key is not exactly 32 bytes.
var ErrInvalidKey = errors.New("crypto: encryption key must be exactly 32 bytes")

// Encryptor performs AES-256-GCM encryption and decryption with a fixed key.
type Encryptor struct {
	block cipher.Block
}

// NewEncryptor creates an Encryptor from a 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != keyLength {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create cipher: %w", err)
	}
	return &Encryptor{block: block}, nil
}

// Encrypt seals plaintext with AES-256-GCM and returns a base64-encoded
// string containing the random nonce followed by the ciphertext.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	gcm, err := cipher.NewGCM(e.block)
	if err != nil {
		return "", fmt.Errorf("crypto: create GCM: %w", err)
	}

	nonce := make([]byte, nonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt opens a base64-encoded AES-256-GCM ciphertext and returns the
// original plaintext.
func (e *Encryptor) Decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("crypto: base64 decode: %w", err)
	}

	gcm, err := cipher.NewGCM(e.block)
	if err != nil {
		return "", fmt.Errorf("crypto: create GCM: %w", err)
	}

	if len(data) < nonceLength {
		return "", errors.New("crypto: ciphertext too short")
	}

	nonce, ciphertext := data[:nonceLength], data[nonceLength:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}

	return string(plaintext), nil
}
