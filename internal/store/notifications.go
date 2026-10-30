package store

import (
	"context"
	"database/sql"
	"fmt"

	"tracker/internal/model"
)

// NotificationStore — доступ к in-app ленте уведомлений.
type NotificationStore struct {
	db DBTX
}

// NewNotificationStore создаёт хранилище поверх *sql.DB или *sql.Tx.
func NewNotificationStore(db DBTX) *NotificationStore {
	return &NotificationStore{db: db}
}

// Create вставляет уведомление.
func (s *NotificationStore) Create(ctx context.Context, n *model.Notification) error {
	const query = `
		INSERT INTO notifications (id, user_id, task_id, kind, title, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := s.db.ExecContext(ctx, query, n.ID, n.UserID, n.TaskID, string(n.Kind), n.Title, n.Body, n.CreatedAt)
	if err != nil {
		return fmt.Errorf("вставка уведомления: %w", err)
	}
	return nil
}

// List возвращает ленту пользователя в порядке убывания created_at.
func (s *NotificationStore) List(ctx context.Context, userID string, unreadOnly bool, limit int) ([]model.Notification, error) {
	const query = `
		SELECT id, task_id, kind, title, body, read_at, created_at
		FROM notifications
		WHERE user_id = $1
		  AND ($2::bool IS FALSE OR read_at IS NULL)
		ORDER BY created_at DESC
		LIMIT $3`

	rows, err := s.db.QueryContext(ctx, query, userID, unreadOnly, limit)
	if err != nil {
		return nil, fmt.Errorf("лента уведомлений: %w", err)
	}
	defer rows.Close()

	items := make([]model.Notification, 0, limit)
	for rows.Next() {
		var n model.Notification
		var taskID sql.NullString
		var readAt sql.NullTime
		if err := rows.Scan(&n.ID, &taskID, &n.Kind, &n.Title, &n.Body, &readAt, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("чтение уведомления: %w", err)
		}
		if taskID.Valid {
			id := taskID.String
			n.TaskID = &id
		}
		if readAt.Valid {
			t := readAt.Time.UTC()
			n.ReadAt = &t
			n.Read = true
		}
		n.UserID = userID
		n.CreatedAt = n.CreatedAt.UTC()
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("обход уведомлений: %w", err)
	}
	return items, nil
}

// CountUnread считает все непрочитанные уведомления пользователя.
func (s *NotificationStore) CountUnread(ctx context.Context, userID string) (int, error) {
	const query = `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`

	var count int
	if err := s.db.QueryRowContext(ctx, query, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("подсчёт непрочитанных: %w", err)
	}
	return count, nil
}

// MarkRead отмечает одно уведомление прочитанным. Нет строки → model.ErrNotFound.
func (s *NotificationStore) MarkRead(ctx context.Context, userID, id string) error {
	const query = `
		UPDATE notifications
		SET read_at = now()
		WHERE id = $1 AND user_id = $2 AND read_at IS NULL`

	res, err := s.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("отметка уведомления: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("проверка числа изменённых строк: %w", err)
	}
	if affected > 0 {
		return nil
	}

	// Строка может быть уже прочитана — это не ошибка, но чужой id должен давать 404.
	const exists = `SELECT 1 FROM notifications WHERE id = $1 AND user_id = $2`
	var one int
	if err := s.db.QueryRowContext(ctx, exists, id, userID).Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return model.ErrNotFound
		}
		return fmt.Errorf("проверка уведомления: %w", err)
	}
	return nil
}

// MarkAllRead отмечает все непрочитанные уведомления пользователя.
func (s *NotificationStore) MarkAllRead(ctx context.Context, userID string) error {
	const query = `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`

	if _, err := s.db.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("отметка всех уведомлений: %w", err)
	}
	return nil
}
