package store

import (
	"context"
	"database/sql"
	"fmt"

	"tracker/internal/model"
)

// ReminderStore — доступ к таблице reminders. Каждый метод скоупится по user_id.
type ReminderStore struct {
	db DBTX
}

// NewReminderStore создаёт хранилище поверх *sql.DB или *sql.Tx.
func NewReminderStore(db DBTX) *ReminderStore {
	return &ReminderStore{db: db}
}

const reminderColumns = `id, task_id, user_id, fire_at, message, status, attempts, sent_at, created_at`

// ListByTask возвращает напоминания задачи пользователя в порядке fire_at.
func (s *ReminderStore) ListByTask(ctx context.Context, userID, taskID string) ([]model.Reminder, error) {
	query := `SELECT ` + reminderColumns + `
		FROM reminders
		WHERE task_id = $1 AND user_id = $2
		ORDER BY fire_at ASC`

	rows, err := s.db.QueryContext(ctx, query, taskID, userID)
	if err != nil {
		return nil, fmt.Errorf("список напоминаний: %w", err)
	}
	defer rows.Close()

	items := make([]model.Reminder, 0, 4)
	for rows.Next() {
		var r model.Reminder
		var sentAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.TaskID, &r.UserID, &r.FireAt, &r.Message,
			&r.Status, &r.Attempts, &sentAt, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("чтение напоминания: %w", err)
		}
		r.FireAt = r.FireAt.UTC()
		r.CreatedAt = r.CreatedAt.UTC()
		if sentAt.Valid {
			t := sentAt.Time.UTC()
			r.SentAt = &t
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("обход напоминаний: %w", err)
	}
	return items, nil
}

// CancelPending переводит все pending-напоминания задачи в expired.
// Вызывается при переводе задачи в done (04-api.md §3).
func (s *ReminderStore) CancelPending(ctx context.Context, userID, taskID string) error {
	const query = `
		UPDATE reminders
		SET status = 'expired'
		WHERE task_id = $1 AND user_id = $2 AND status = 'pending'`

	if _, err := s.db.ExecContext(ctx, query, taskID, userID); err != nil {
		return fmt.Errorf("отмена напоминаний: %w", err)
	}
	return nil
}
