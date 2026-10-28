package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tracker/internal/model"
)

// TaskStore — доступ к таблице tasks. Каждый метод скоупится по user_id.
type TaskStore struct {
	db DBTX
}

// NewTaskStore создаёт хранилище поверх *sql.DB или *sql.Tx.
func NewTaskStore(db DBTX) *TaskStore {
	return &TaskStore{db: db}
}

// TaskFilter — параметры выборки списка задач (docs/03 §4).
type TaskFilter struct {
	Status   model.Status
	Priority model.Priority
	Search   string
	Sort     string
	Dir      string
	Limit    int
	Offset   int
}

// Порядок сортировки: только из белого списка, никакой пользовательской строки в SQL.
var sortColumns = map[string]string{
	"due_at":     "COALESCE(due_at, 'infinity'::timestamptz)",
	"created_at": "created_at",
	"priority":   "CASE priority WHEN 'high' THEN 3 WHEN 'medium' THEN 2 ELSE 1 END",
}

const taskColumns = `
	id, title, description, status, priority, due_at, created_at, updated_at,
	(SELECT count(*) FROM reminders r WHERE r.task_id = tasks.id AND r.status = 'pending') AS reminder_count`

// Create вставляет задачу.
func (s *TaskStore) Create(ctx context.Context, t *model.Task) error {
	const query = `
		INSERT INTO tasks (id, user_id, title, description, status, priority, due_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`

	_, err := s.db.ExecContext(ctx, query,
		t.ID, t.UserID, t.Title, t.Description, string(t.Status), string(t.Priority), t.DueAt, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("вставка задачи: %w", err)
	}
	return nil
}

// Get возвращает задачу пользователя. Чужая задача неотличима от несуществующей.
func (s *TaskStore) Get(ctx context.Context, userID, id string) (*model.Task, error) {
	query := `SELECT` + taskColumns + ` FROM tasks WHERE id = $1 AND user_id = $2`

	t, err := scanTask(s.db.QueryRowContext(ctx, query, id, userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("чтение задачи: %w", err)
	}
	t.UserID = userID
	return t, nil
}

// List возвращает страницу задач и общее количество по фильтру.
func (s *TaskStore) List(ctx context.Context, userID string, f TaskFilter) ([]model.Task, int, error) {
	orderColumn, ok := sortColumns[f.Sort]
	if !ok {
		return nil, 0, fmt.Errorf("неизвестная сортировка: %q", f.Sort)
	}
	direction := "ASC"
	if f.Dir == "desc" {
		direction = "DESC"
	}

	where := `
		WHERE user_id = $1
		  AND ($2::text IS NULL OR status   = $2)
		  AND ($3::text IS NULL OR priority = $3)
		  AND ($4::text IS NULL OR title ILIKE '%' || $4 || '%')`

	args := []any{userID, nullableText(string(f.Status)), nullableText(string(f.Priority)), nullableText(f.Search)}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tasks`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("подсчёт задач: %w", err)
	}

	query := `SELECT` + taskColumns + ` FROM tasks` + where +
		` ORDER BY ` + orderColumn + ` ` + direction + `, created_at DESC LIMIT $5 OFFSET $6`

	rows, err := s.db.QueryContext(ctx, query, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("список задач: %w", err)
	}
	defer rows.Close()

	tasks := make([]model.Task, 0, f.Limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("чтение строки задачи: %w", err)
		}
		t.UserID = userID
		tasks = append(tasks, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("обход задач: %w", err)
	}
	return tasks, total, nil
}

// Update перезаписывает редактируемые поля задачи. Нет строки → model.ErrNotFound.
func (s *TaskStore) Update(ctx context.Context, t *model.Task) error {
	const query = `
		UPDATE tasks
		SET title = $1, description = $2, priority = $3, due_at = $4, updated_at = now()
		WHERE id = $5 AND user_id = $6`

	res, err := s.db.ExecContext(ctx, query, t.Title, t.Description, string(t.Priority), t.DueAt, t.ID, t.UserID)
	if err != nil {
		return fmt.Errorf("обновление задачи: %w", err)
	}
	return requireAffected(res)
}

// Delete удаляет задачу; напоминания и уведомления уходят каскадом.
func (s *TaskStore) Delete(ctx context.Context, userID, id string) error {
	const query = `DELETE FROM tasks WHERE id = $1 AND user_id = $2`

	res, err := s.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("удаление задачи: %w", err)
	}
	return requireAffected(res)
}

// UpdateStatus меняет статус задачи. Нет строки → model.ErrNotFound.
func (s *TaskStore) UpdateStatus(ctx context.Context, userID, id string, status model.Status) error {
	const query = `
		UPDATE tasks
		SET status = $1, updated_at = now()
		WHERE id = $2 AND user_id = $3`

	res, err := s.db.ExecContext(ctx, query, string(status), id, userID)
	if err != nil {
		return fmt.Errorf("смена статуса задачи: %w", err)
	}
	return requireAffected(res)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (*model.Task, error) {
	var t model.Task
	var due sql.NullTime
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&due, &t.CreatedAt, &t.UpdatedAt, &t.ReminderCount)
	if err != nil {
		return nil, err
	}
	if due.Valid {
		d := due.Time.UTC()
		t.DueAt = &d
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return &t, nil
}

func nullableText(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("проверка числа изменённых строк: %w", err)
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}
