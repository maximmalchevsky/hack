package auth

import "testing"

func TestHashVerifyPassword(t *testing.T) {
	const pw = "qwerty12345"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == pw {
		t.Error("хеш совпадает с паролем — не захеширован")
	}
	if hash == "" {
		t.Error("пустой хеш")
	}
	if !VerifyPassword(hash, pw) {
		t.Error("VerifyPassword вернул false для правильного пароля")
	}
	if VerifyPassword(hash, "wrong") {
		t.Error("VerifyPassword вернул true для неправильного пароля")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Error("два хеша одного пароля совпали — нет соли (bcrypt должен солить)")
	}
}

func TestVerifyPasswordRejectsBadHash(t *testing.T) {
	if VerifyPassword("не-bcrypt-хеш", "any") {
		t.Error("VerifyPassword с мусорным хешем должен вернуть false")
	}
}
