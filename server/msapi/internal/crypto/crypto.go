// Package crypto encrypts store secrets at rest with AES-256-GCM (PLAN.md §8).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Box encrypts and decrypts small secrets. Output layout: nonce || ciphertext.
type Box struct {
	aead cipher.AEAD
}

func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. An empty plaintext returns nil (no secret stored).
func (b *Box) Seal(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Open decrypts a value made by Seal. nil returns "".
func (b *Box) Open(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return "", errors.New("secret too short")
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return "", errors.New("cannot decrypt secret (was MSAPI_SECRET_KEY changed?)")
	}
	return string(plain), nil
}
