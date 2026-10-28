// Package store — единственное место с SQL. Никаких бизнес-решений и валидации.
package store

import (
	"context"
	"database/sql"
	"fmt"

	// Драйвер Postgres через database/sql (T-5).
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DBTX — общий набор методов *sql.DB и *sql.Tx.
// Позволяет сервису собирать транзакцию из вызовов store.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open открывает пул соединений и проверяет доступность БД.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("открытие БД: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping БД: %w", err)
	}
	return db, nil
}

// Migrate применяет идемпотентный DDL из db/schema.sql одной транзакцией (T-6).
func Migrate(ctx context.Context, db *sql.DB, schema string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("начало транзакции схемы: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("применение схемы: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("коммит схемы: %w", err)
	}
	return nil
}
