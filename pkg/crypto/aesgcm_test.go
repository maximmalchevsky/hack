package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	c, err := NewFromBase64(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewFromBase64: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	cases := []string{"", "secret-token", "длинная строка с юникодом 😀 и пробелами"}
	for _, plain := range cases {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		dec, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != plain {
			t.Errorf("round-trip: got %q, want %q", dec, plain)
		}
	}
}

func TestEncryptIsRandomized(t *testing.T) {
	c := newTestCipher(t)
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Error("два шифрования одного текста совпали — nonce не рандомный")
	}
}

func TestNewFromBase64Errors(t *testing.T) {
	if _, err := NewFromBase64(""); err == nil {
		t.Error("пустой ключ должен давать ошибку")
	}
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := NewFromBase64(short); err == nil {
		t.Error("ключ неверной длины должен давать ошибку")
	}
	if _, err := NewFromBase64("не base64 !!!"); err == nil {
		t.Error("невалидный base64 должен давать ошибку")
	}
}

func TestDecryptRejectsGarbage(t *testing.T) {
	c := newTestCipher(t)
	if _, err := c.Decrypt("не валидный шифртекст"); err == nil {
		t.Error("Decrypt мусора должен возвращать ошибку")
	}
}
