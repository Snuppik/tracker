package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"tracker/internal/model"
)

// UserStore — доступ к таблице users.
type UserStore struct {
	db DBTX
}

// NewUserStore создаёт хранилище поверх *sql.DB или *sql.Tx.
func NewUserStore(db DBTX) *UserStore {
	return &UserStore{db: db}
}

// Create вставляет пользователя. Дубликат email → model.ErrConflict.
func (s *UserStore) Create(ctx context.Context, u *model.User) error {
	const query = `
		INSERT INTO users (id, email, name, password_hash, created_at)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := s.db.ExecContext(ctx, query, u.ID, u.Email, u.Name, u.PasswordHash, u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return model.ErrConflict
		}
		return fmt.Errorf("вставка пользователя: %w", err)
	}
	return nil
}

// ByEmail ищет пользователя по нормализованному email. Нет записи → model.ErrNotFound.
func (s *UserStore) ByEmail(ctx context.Context, email string) (*model.User, error) {
	const query = `
		SELECT id, email, name, password_hash, created_at
		FROM users
		WHERE email = $1`

	var u model.User
	err := s.db.QueryRowContext(ctx, query, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("поиск пользователя по email: %w", err)
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return &u, nil
}

// ByID ищет пользователя по идентификатору. Нет записи → model.ErrNotFound.
func (s *UserStore) ByID(ctx context.Context, id string) (*model.User, error) {
	const query = `
		SELECT id, email, name, password_hash, created_at
		FROM users
		WHERE id = $1`

	var u model.User
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("поиск пользователя по id: %w", err)
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return &u, nil
}

// isUniqueViolation распознаёт нарушение UNIQUE (Postgres 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
