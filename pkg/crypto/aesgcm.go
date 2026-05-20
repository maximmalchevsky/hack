// Package crypto — AES-256-GCM шифрование строк для хранения OAuth-токенов в БД.
//
// Использование:
//
//	c, err := crypto.NewFromBase64(os.Getenv("APP_ENCRYPTION_KEY"))
//	enc := c.Encrypt("super-secret-refresh-token")     // -> base64
//	dec, _ := c.Decrypt(enc)
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const keySize = 32 // AES-256

// Cipher — обёртка над AES-GCM с фиксированным ключом.
type Cipher struct {
	aead cipher.AEAD
}

// NewFromBase64 — создаёт Cipher из base64-ключа (32 байта после декодирования).
func NewFromBase64(keyB64 string) (*Cipher, error) {
	if keyB64 == "" {
		return nil, errors.New("crypto: empty key")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode key: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("crypto: key must be %d bytes, got %d", keySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt — возвращает base64(nonce || ciphertext || tag).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt — расшифровывает строку, ранее полученную из Encrypt.
func (c *Cipher) Decrypt(encB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encB64)
	if err != nil {
		return "", fmt.Errorf("crypto: decode b64: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("crypto: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: gcm open: %w", err)
	}
	return string(plain), nil
}
