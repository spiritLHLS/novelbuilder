package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// Encrypt encrypts plaintext with AES-256-GCM using key (any length; SHA-256 derived).
// If key is empty, returns plaintext unchanged (dev mode).
// Output is base64url(nonce || ciphertext).
func Encrypt(plaintext, key string) (string, error) {
	if key == "" {
		return plaintext, nil
	}

	block, err := newCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt reverses Encrypt. If key is empty, returns ciphertext unchanged.
// If decryption fails (e.g. unencrypted legacy value), returns input as-is.
func Decrypt(ciphertext, key string) (string, error) {
	if key == "" {
		return ciphertext, nil
	}

	data, err := base64.URLEncoding.DecodeString(ciphertext)
	if err != nil {
		// Not base64 -> treat as unencrypted legacy value
		return ciphertext, nil
	}

	block, err := newCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		// Too short -> probably plain text stored before encryption was enabled
		return ciphertext, nil
	}

	nonce, data := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, data, nil)
	if err != nil {
		// Decryption failure -> return original (handles unencrypted legacy values)
		return ciphertext, nil
	}

	return string(plaintext), nil
}

// newCipher creates an AES block cipher with a 32-byte key derived via SHA-256.
func newCipher(key string) (cipher.Block, error) {
	h := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(h[:])
	if err != nil {
		return nil, errors.New("crypto: failed to create cipher: " + err.Error())
	}
	return block, nil
}
