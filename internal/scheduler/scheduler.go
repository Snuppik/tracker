// Package scheduler — фоновый планировщик напоминаний (02-architecture.md §5).
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"tracker/internal/model"
	"tracker/internal/push"
	"tracker/internal/store"
)

// batchSize — сколько напоминаний обрабатываем за один тик.
const batchSize = 50

// Scheduler забирает созревшие напоминания и превращает их в уведомления.
type Scheduler struct {
	db     *sql.DB
	log    *slog.Logger
	sender *push.Sender
	tick   time.Duration
	grace  time.Duration
}

// New собирает планировщик.
func New(db *sql.DB, log *slog.Logger, sender *push.Sender, tick, grace time.Duration) *Scheduler {
	return &Scheduler{db: db, log: log, sender: sender, tick: tick, grace: grace}
}

// Run блокируется до отмены контекста.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	s.log.Info("планировщик напоминаний запущен", "tick", s.tick, "grace", s.grace)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("планировщик остановлен")
			return
		case <-ticker.C:
			s.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce обрабатывает одну порцию созревших напоминаний.
// Экспортирован для тестов: не требует ожидания тикера.
func (s *Scheduler) ProcessOnce(ctx context.Context) {
	for i := 0; i < batchSize; i++ {
		processed, err := s.processOne(ctx)
		if err != nil {
			s.log.Error("обработка напоминания", "error", err)
			return
		}
		if !processed {
			return
		}
	}
}

// processOne берёт одно напоминание в транзакции и доводит до sent/expired.
// Возвращает false, если очередь пуста.
func (s *Scheduler) processOne(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("начало транзакции планировщика: %w", err)
	}
	defer tx.Rollback()

	const pickQuery = `
		SELECT id, task_id, user_id, fire_at, message
		FROM reminders
		WHERE status = 'pending' AND fire_at <= $1
		ORDER BY fire_at
		LIMIT 1
		FOR UPDATE SKIP LOCKED`

	now := time.Now().UTC()

	var r model.Reminder
	err = tx.QueryRowContext(ctx, pickQuery, now).
		Scan(&r.ID, &r.TaskID, &r.UserID, &r.FireAt, &r.Message)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("выборка напоминания: %w", err)
	}
	r.FireAt = r.FireAt.UTC()

	// Grace window: сервер мог быть выключен. Старые напоминания гасим без уведомления.
	if now.Sub(r.FireAt) > s.grace {
		if err := s.markExpired(ctx, tx, r.ID); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("коммит expired: %w", err)
		}
		s.log.Info("напоминание просрочено (grace window)", "reminder_id", r.ID)
		return true, nil
	}

	// Задача проверяется в той же транзакции: пользователь мог закрыть её только что.
	const taskQuery = `SELECT title, status FROM tasks WHERE id = $1 FOR UPDATE`
	var taskTitle, taskStatus string
	err = tx.QueryRowContext(ctx, taskQuery, r.TaskID).Scan(&taskTitle, &taskStatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := s.markExpired(ctx, tx, r.ID); err != nil {
				return false, err
			}
			if err := tx.Commit(); err != nil {
				return false, fmt.Errorf("коммит expired: %w", err)
			}
			return true, nil
		}
		return false, fmt.Errorf("чтение задачи напоминания: %w", err)
	}

	if model.Status(taskStatus) == model.StatusDone {
		if err := s.markExpired(ctx, tx, r.ID); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("коммит expired: %w", err)
		}
		return true, nil
	}

	body := r.Message
	if body == "" {
		body = "Напоминание по задаче"
	}
	notification := &model.Notification{
		ID:        uuid.NewV7().String(),
		UserID:    r.UserID,
		TaskID:    &r.TaskID,
		Kind:      model.NotificationReminder,
		Title:     taskTitle,
		Body:      body,
		CreatedAt: now,
	}
	if err := store.NewNotificationStore(tx).Create(ctx, notification); err != nil {
		return false, err
	}

	const markSent = `
		UPDATE reminders
		SET status = 'sent', sent_at = $1, attempts = 0
		WHERE id = $2 AND status = 'pending'`
	if _, err := tx.ExecContext(ctx, markSent, now, r.ID); err != nil {
		return false, fmt.Errorf("отметка напоминания отправленным: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("коммит напоминания: %w", err)
	}

	s.log.Info("напоминание доставлено в ленту",
		"reminder_id", r.ID, "user_id", r.UserID, "notification_id", notification.ID)

	s.sendPush(ctx, r.UserID, taskTitle, body, r.TaskID)
	return true, nil
}

// sendPush выполняется после коммита: в ленте уже есть запись, на которую
// можно сослаться из notificationclick.
func (s *Scheduler) sendPush(ctx context.Context, userID, title, body, taskID string) {
	if !s.sender.Enabled() {
		return
	}

	subscriptions, err := store.NewPushStore(s.db).ListByUser(ctx, userID)
	if err != nil {
		s.log.Error("чтение push-подписок", "user_id", userID, "error", err)
		return
	}

	payload := push.Payload{Title: title, Body: body, TaskID: taskID}
	for _, sub := range subscriptions {
		err := s.sender.Send(ctx, sub, payload)
		switch {
		case err == nil:
		case errors.Is(err, push.ErrSubscriptionGone):
			s.log.Info("удаляю мёртвую push-подписку", "endpoint", sub.Endpoint)
			if delErr := store.NewPushStore(s.db).DeleteByEndpoint(ctx, userID, sub.Endpoint); delErr != nil {
				s.log.Error("удаление мёртвой подписки", "error", delErr)
			}
		default:
			s.log.Warn("не удалось отправить push", "endpoint", sub.Endpoint, "error", err)
		}
	}
}

func (s *Scheduler) markExpired(ctx context.Context, tx *sql.Tx, reminderID string) error {
	const query = `UPDATE reminders SET status = 'expired' WHERE id = $1 AND status = 'pending'`
	if _, err := tx.ExecContext(ctx, query, reminderID); err != nil {
		return fmt.Errorf("пометка напоминания expired: %w", err)
	}
	return nil
}
