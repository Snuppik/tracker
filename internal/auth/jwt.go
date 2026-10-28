package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenManager выпускает и проверяет JWT (HS256, T-9).
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenManager создаёт менеджер токенов. Пустой секрет недопустим.
func NewTokenManager(secret []byte, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: secret, ttl: ttl}
}

// Issue выпускает токен для пользователя.
func (m *TokenManager) Issue(userID string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("подпись токена: %w", err)
	}
	return token, nil
}

// Parse проверяет подпись и срок действия и возвращает идентификатор пользователя.
func (m *TokenManager) Parse(token string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return "", fmt.Errorf("недействительный токен: %w", err)
	}
	if claims.Subject == "" {
		return "", fmt.Errorf("недействительный токен: пустой subject")
	}
	return claims.Subject, nil
}
