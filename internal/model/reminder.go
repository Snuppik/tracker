package model

import "time"

// ReminderStatus — статус напоминания. Меняется только планировщиком.
type ReminderStatus string

const (
	ReminderPending ReminderStatus = "pending"
	ReminderSent    ReminderStatus = "sent"
	ReminderExpired ReminderStatus = "expired"
	ReminderFailed  ReminderStatus = "failed"
)

// Reminder — напоминание к задаче.
type Reminder struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"task_id"`
	UserID    string         `json:"-"`
	FireAt    time.Time      `json:"fire_at"`
	Message   string         `json:"message"`
	Status    ReminderStatus `json:"status"`
	Attempts  int            `json:"-"`
	SentAt    *time.Time     `json:"-"`
	CreatedAt time.Time      `json:"created_at"`
}

// NotificationKind — вид уведомления в ленте.
type NotificationKind string

const (
	NotificationReminder NotificationKind = "reminder"
	NotificationSystem   NotificationKind = "system"
)

// Notification — запись in-app ленты. Источник истины для доставки.
type Notification struct {
	ID        string           `json:"id"`
	UserID    string           `json:"-"`
	TaskID    *string          `json:"task_id"`
	Kind      NotificationKind `json:"kind"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	Read      bool             `json:"read"`
	ReadAt    *time.Time       `json:"-"`
	CreatedAt time.Time        `json:"created_at"`
}
