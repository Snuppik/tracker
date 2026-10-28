// Package service — бизнес-правила и валидация. Не знает про HTTP.
package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"tracker/internal/auth"
	"tracker/internal/model"
	"tracker/internal/store"
)

// emailPattern — упрощённая проверка из docs/04-api.md §2.
var emailPattern = regexp.MustCompile(`^.+@.+\..+$`)

// AuthService — регистрация, вход, чтение текущего пользователя.
type AuthService struct {
	users  *store.UserStore
	tokens *auth.TokenManager
}

// NewAuthService собирает сервис аутентификации.
func NewAuthService(users *store.UserStore, tokens *auth.TokenManager) *AuthService {
	return &AuthService{users: users, tokens: tokens}
}

// Register создаёт пользователя и выпускает токен.
func (s *AuthService) Register(ctx context.Context, email, password, name string) (*model.User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)

	if err := validateRegistration(email, password, name); err != nil {
		return nil, "", err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, "", err
	}

	user := &model.User{
		ID:           uuid.NewV7().String(),
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, "", err
	}

	token, err := s.tokens.Issue(user.ID)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// Login проверяет пару email/пароль. Любая несоответствие → model.ErrUnauthorized,
// чтобы нельзя было отличить «нет пользователя» от «неверный пароль».
func (s *AuthService) Login(ctx context.Context, email, password string) (*model.User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, "", model.NewValidation("email", "обязательное поле")
	}
	if password == "" {
		return nil, "", model.NewValidation("password", "обязательное поле")
	}

	user, err := s.users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, "", model.ErrUnauthorized
		}
		return nil, "", err
	}
	if !auth.CheckPassword(user.PasswordHash, password) {
		return nil, "", model.ErrUnauthorized
	}

	token, err := s.tokens.Issue(user.ID)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// User возвращает пользователя по идентификатору из токена.
func (s *AuthService) User(ctx context.Context, id string) (*model.User, error) {
	return s.users.ByID(ctx, id)
}

func validateRegistration(email, password, name string) error {
	ve := &model.ValidationError{}

	if email == "" {
		ve.Add("email", "обязательное поле")
	} else if len(email) > 254 {
		ve.Add("email", "не длиннее 254 символов")
	} else if !emailPattern.MatchString(email) {
		ve.Add("email", "неверный формат email")
	}

	if password == "" {
		ve.Add("password", "обязательное поле")
	} else if len(password) < 8 {
		ve.Add("password", "не короче 8 символов")
	} else if len(password) > 72 {
		ve.Add("password", "не длиннее 72 байт")
	}

	switch {
	case name == "":
		ve.Add("name", "обязательное поле")
	case utf8.RuneCountInString(name) > 100:
		ve.Add("name", "не длиннее 100 символов")
	}

	if ve.Empty() {
		return nil
	}
	return ve
}
