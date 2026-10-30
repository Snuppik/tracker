package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"tracker/internal/model"
	"tracker/internal/store"
)

// MaxPendingReminders — лимит активных напоминаний на задачу (04-api.md §4).
const MaxPendingReminders = 5

// reminderPastTolerance — допуск на расхождение часов браузера и сервера.
const reminderPastTolerance = time.Minute

// reminderMaxAhead — верхняя граница срока напоминания.
const reminderMaxAhead = 5 * 365 * 24 * time.Hour

// ReminderService — правила напоминаний: лимит, окно fire_at, скоуп по пользователю.
type ReminderService struct {
	db *sql.DB
}

// NewReminderService собирает сервис напоминаний.
func NewReminderService(db *sql.DB) *ReminderService {
	return &ReminderService{db: db}
}

// ReminderCreateInput — проверенные данные создания напоминания.
type ReminderCreateInput struct {
	FireAt  time.Time
	Message string
}

// List возвращает напоминания задачи пользователя.
func (s *ReminderService) List(ctx context.Context, userID, taskID string) ([]model.Reminder, error) {
	if _, err := store.NewTaskStore(s.db).Get(ctx, userID, taskID); err != nil {
		return nil, err
	}
	return store.NewReminderStore(s.db).ListByTask(ctx, userID, taskID)
}

// Create добавляет напоминание к задаче пользователя.
// Проверка лимита и вставка идут в одной транзакции с блокировкой задачи,
// чтобы параллельные запросы не пробили лимит в 5.
func (s *ReminderService) Create(ctx context.Context, userID, taskID string, in ReminderCreateInput) (*model.Reminder, error) {
	in.Message = strings.TrimSpace(in.Message)

	if err := validateReminder(in, time.Now().UTC()); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("начало транзакции напоминания: %w", err)
	}
	defer tx.Rollback()

	// Блокируем строку задачи: она же подтверждает владение.
	const lockTask = `SELECT id FROM tasks WHERE id = $1 AND user_id = $2 FOR UPDATE`
	var lockedID string
	if err := tx.QueryRowContext(ctx, lockTask, taskID, userID).Scan(&lockedID); err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("блокировка задачи: %w", err)
	}

	reminders := store.NewReminderStore(tx)
	pending, err := reminders.CountPending(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if pending >= MaxPendingReminders {
		return nil, model.ErrLimitExceeded
	}

	reminder := &model.Reminder{
		ID:        uuid.NewV7().String(),
		TaskID:    taskID,
		UserID:    userID,
		FireAt:    in.FireAt.UTC(),
		Message:   in.Message,
		Status:    model.ReminderPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := reminders.Create(ctx, reminder); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("коммит напоминания: %w", err)
	}
	return reminder, nil
}

// Delete удаляет напоминание пользователя.
func (s *ReminderService) Delete(ctx context.Context, userID, id string) error {
	return store.NewReminderStore(s.db).Delete(ctx, userID, id)
}

func validateReminder(in ReminderCreateInput, now time.Time) error {
	ve := &model.ValidationError{}

	if in.FireAt.IsZero() {
		ve.Add("fire_at", "обязательное поле")
	} else {
		fireAt := in.FireAt.UTC()
		if fireAt.Before(now.Add(-reminderPastTolerance)) {
			ve.Add("fire_at", "время должно быть в будущем")
		}
		if fireAt.After(now.Add(reminderMaxAhead)) {
			ve.Add("fire_at", "слишком далёкая дата")
		}
	}

	if utf8.RuneCountInString(in.Message) > 200 {
		ve.Add("message", "не длиннее 200 символов")
	}

	if ve.Empty() {
		return nil
	}
	return ve
}
