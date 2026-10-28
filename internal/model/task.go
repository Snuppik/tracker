package model

import "time"

// Status — статус задачи. Ровно три значения (P-3).
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// Valid сообщает, входит ли значение в разрешённый набор.
func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusDone:
		return true
	default:
		return false
	}
}

// Priority — приоритет задачи. Ровно три значения (P-4).
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

// Valid сообщает, входит ли значение в разрешённый набор.
func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh:
		return true
	default:
		return false
	}
}

// Task — задача пользователя. Вычисляемые флаги заполняет сервис.
type Task struct {
	ID            string     `json:"id"`
	UserID        string     `json:"-"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        Status     `json:"status"`
	Priority      Priority   `json:"priority"`
	DueAt         *time.Time `json:"due_at"`
	Overdue       bool       `json:"overdue"`
	DueSoon       bool       `json:"due_soon"`
	ReminderCount int        `json:"reminder_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// TaskWithReminders — деталь задачи вместе с напоминаниями (GET /api/tasks/{id}).
type TaskWithReminders struct {
	Task
	Reminders []Reminder `json:"reminders"`
}

// ComputeFlags пересчитывает overdue/due_soon от указанного момента.
// Правило одно на весь бэкенд: due_at < now — просрочено; now ≤ due_at < now+24h — горит.
// Для done оба флага всегда false.
func (t *Task) ComputeFlags(now time.Time) {
	t.Overdue = false
	t.DueSoon = false
	if t.DueAt == nil || t.Status == StatusDone {
		return
	}
	due := t.DueAt.UTC()
	if due.Before(now) {
		t.Overdue = true
		return
	}
	t.DueSoon = due.Before(now.Add(24 * time.Hour))
}
