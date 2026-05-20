package auth

import "golang.org/x/crypto/bcrypt"

const bcryptCost = bcrypt.DefaultCost

// HashPassword — bcrypt-хеш пароля.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword — true если пароль совпадает с хешем.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
