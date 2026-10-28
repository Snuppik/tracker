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

// TaskService — бизнес-правила задач: валидация, скоуп по пользователю, транзакции.
type TaskService struct {
	db    *sql.DB
	tasks *store.TaskStore
}

// NewTaskService собирает сервис задач.
func NewTaskService(db *sql.DB) *TaskService {
	return &TaskService{db: db, tasks: store.NewTaskStore(db)}
}

// TaskCreateInput — проверенные данные создания задачи.
type TaskCreateInput struct {
	Title       string
	Description string
	Priority    model.Priority
	Status      model.Status
	DueAt       *time.Time
}

// TaskPatch — частичное обновление: nil означает «поле не передано».
type TaskPatch struct {
	Title       *string
	Description *string
	Priority    *model.Priority
	DueAtSet    bool
	DueAt       *time.Time
}

// ListParams — сырые query-параметры списка задач.
type ListParams struct {
	Status   string
	Priority string
	Search   string
	Sort     string
	Dir      string
	Limit    int
	Offset   int
}

// TaskList — страница списка задач вместе с параметрами пагинации.
type TaskList struct {
	Items  []model.Task
	Total  int
	Limit  int
	Offset int
}

// List возвращает страницу задач и общее количество.
func (s *TaskService) List(ctx context.Context, userID string, p ListParams) (*TaskList, error) {
	filter, err := buildFilter(p)
	if err != nil {
		return nil, err
	}

	tasks, total, err := s.tasks.List(ctx, userID, filter)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for i := range tasks {
		tasks[i].ComputeFlags(now)
	}
	return &TaskList{Items: tasks, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

// Create создаёт задачу пользователя.
func (s *TaskService) Create(ctx context.Context, userID string, in TaskCreateInput) (*model.Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Priority == "" {
		in.Priority = model.PriorityMedium
	}
	if in.Status == "" {
		in.Status = model.StatusTodo
	}
	if err := validateTask(in.Title, in.Description, in.Priority, in.Status, in.DueAt); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	task := &model.Task{
		ID:          uuid.NewV7().String(),
		UserID:      userID,
		Title:       in.Title,
		Description: in.Description,
		Status:      in.Status,
		Priority:    in.Priority,
		DueAt:       in.DueAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	task.ComputeFlags(now)
	return task, nil
}

// Get возвращает задачу вместе с напоминаниями.
func (s *TaskService) Get(ctx context.Context, userID, id string) (*model.TaskWithReminders, error) {
	task, err := s.tasks.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	task.ComputeFlags(time.Now().UTC())

	reminders, err := store.NewReminderStore(s.db).ListByTask(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return &model.TaskWithReminders{Task: *task, Reminders: reminders}, nil
}

// Update применяет частичное обновление. Отсутствующие поля не меняются.
func (s *TaskService) Update(ctx context.Context, userID, id string, patch TaskPatch) (*model.Task, error) {
	task, err := s.tasks.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	if patch.Title != nil {
		task.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Description != nil {
		task.Description = *patch.Description
	}
	if patch.Priority != nil {
		task.Priority = *patch.Priority
	}
	if patch.DueAtSet {
		task.DueAt = patch.DueAt
	}

	if err := validateTask(task.Title, task.Description, task.Priority, task.Status, task.DueAt); err != nil {
		return nil, err
	}
	if err := s.tasks.Update(ctx, task); err != nil {
		return nil, err
	}

	updated, err := s.tasks.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	updated.ComputeFlags(time.Now().UTC())
	return updated, nil
}

// Delete удаляет задачу пользователя.
func (s *TaskService) Delete(ctx context.Context, userID, id string) error {
	return s.tasks.Delete(ctx, userID, id)
}

// ChangeStatus меняет статус. При переходе в done pending-напоминания гасятся
// в той же транзакции (04-api.md §3).
func (s *TaskService) ChangeStatus(ctx context.Context, userID, id string, status model.Status) (*model.Task, error) {
	if !status.Valid() {
		return nil, model.NewValidation("status", "допустимо: todo, in_progress, done")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("начало транзакции смены статуса: %w", err)
	}
	defer tx.Rollback()

	tasks := store.NewTaskStore(tx)
	if err := tasks.UpdateStatus(ctx, userID, id, status); err != nil {
		return nil, err
	}
	if status == model.StatusDone {
		if err := store.NewReminderStore(tx).CancelPending(ctx, userID, id); err != nil {
			return nil, err
		}
	}

	task, err := tasks.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("коммит смены статуса: %w", err)
	}

	task.ComputeFlags(time.Now().UTC())
	return task, nil
}

func buildFilter(p ListParams) (store.TaskFilter, error) {
	ve := &model.ValidationError{}

	var status model.Status
	if raw := strings.TrimSpace(p.Status); raw != "" {
		status = model.Status(raw)
		if !status.Valid() {
			ve.Add("status", "допустимо: todo, in_progress, done")
		}
	}

	var priority model.Priority
	if raw := strings.TrimSpace(p.Priority); raw != "" {
		priority = model.Priority(raw)
		if !priority.Valid() {
			ve.Add("priority", "допустимо: low, medium, high")
		}
	}

	sort := strings.TrimSpace(p.Sort)
	if sort == "" {
		sort = "due_at"
	}
	switch sort {
	case "due_at", "created_at", "priority":
	default:
		ve.Add("sort", "допустимо: due_at, created_at, priority")
	}

	dir := strings.ToLower(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = "asc"
	}
	switch dir {
	case "asc", "desc":
	default:
		ve.Add("dir", "допустимо: asc, desc")
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	if !ve.Empty() {
		return store.TaskFilter{}, ve
	}

	return store.TaskFilter{
		Status:   status,
		Priority: priority,
		Search:   strings.TrimSpace(p.Search),
		Sort:     sort,
		Dir:      dir,
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func validateTask(title, description string, priority model.Priority, status model.Status, dueAt *time.Time) error {
	ve := &model.ValidationError{}

	switch {
	case strings.TrimSpace(title) == "":
		ve.Add("title", "обязательное поле")
	case utf8.RuneCountInString(title) > 200:
		ve.Add("title", "не длиннее 200 символов")
	}

	if utf8.RuneCountInString(description) > 2000 {
		ve.Add("description", "не длиннее 2000 символов")
	}

	if !priority.Valid() {
		ve.Add("priority", "допустимо: low, medium, high")
	}
	if !status.Valid() {
		ve.Add("status", "допустимо: todo, in_progress, done")
	}
	if dueAt != nil {
		if dueAt.IsZero() {
			ve.Add("due_at", "неверный формат, ожидается RFC3339")
		}
	}

	if ve.Empty() {
		return nil
	}
	return ve
}
