// Package auth — пароли, JWT и перенос идентификатора пользователя в контексте.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost зафиксирован решением T-10.
const bcryptCost = 10

// HashPassword возвращает bcrypt-хэш пароля.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("хэширование пароля: %w", err)
	}
	return string(hash), nil
}

// CheckPassword сравнивает пароль с хэшем. Ошибка означает «не совпало».
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
