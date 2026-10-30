// Package insights — детерминированный rule-based движок подсказок (docs/07-ai-insights.md).
// Никаких внешних LLM: одинаковый вход даёт одинаковый выход.
package insights

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Уровни важности подсказки.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Максимум подсказок в ответе (docs/07-ai-insights.md §3).
const maxItems = 5

// Insight — одна подсказка.
type Insight struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	TaskIDs  []string `json:"task_ids"`
}

// List — ответ GET /api/insights.
type List struct {
	GeneratedAt time.Time `json:"generated_at"`
	Items       []Insight `json:"items"`
}

// Collect строит подсказки для пользователя.
func Collect(ctx context.Context, db *sql.DB, userID string) (*List, error) {
	return collectAt(ctx, db, userID, time.Now().UTC())
}

// collectAt — тот же движок с явным «сейчас» для воспроизводимых тестов.
func collectAt(ctx context.Context, db *sql.DB, userID string, now time.Time) (*List, error) {
	rules := []func(context.Context, *sql.DB, string, time.Time) (*Insight, error){
		ruleNoTasks,
		ruleOverdue,
		ruleDueSoonCluster,
		ruleOverloadedDay,
		ruleNoDeadline,
		ruleStaleInProgress,
		ruleHighPriorityInTodo,
		ruleWeeklyProgress,
	}

	items := make([]Insight, 0, maxItems)
	for _, rule := range rules {
		if len(items) >= maxItems {
			break
		}
		insight, err := rule(ctx, db, userID, now)
		if err != nil {
			return nil, err
		}
		if insight != nil {
			items = append(items, *insight)
		}
	}

	return &List{GeneratedAt: now, Items: items}, nil
}

// R-1: no_tasks — у пользователя нет ни одной задачи (онбординг).
func ruleNoTasks(ctx context.Context, db *sql.DB, userID string, _ time.Time) (*Insight, error) {
	const query = `SELECT count(*) FROM tasks WHERE user_id = $1`

	var total int
	if err := db.QueryRowContext(ctx, query, userID).Scan(&total); err != nil {
		return nil, fmt.Errorf("R-1 подсчёт задач: %w", err)
	}
	if total > 0 {
		return nil, nil
	}

	return &Insight{
		Code:     "no_tasks",
		Severity: SeverityInfo,
		Title:    "Начните с первой задачи",
		Message:  "Создайте задачу с дедлайном — она появится на доске, а подсказки станут полезнее.",
		TaskIDs:  []string{},
	}, nil
}

// R-2: overdue_exists — есть просроченные незавершённые задачи.
func ruleOverdue(ctx context.Context, db *sql.DB, userID string, now time.Time) (*Insight, error) {
	const query = `
		SELECT id FROM tasks
		WHERE user_id = $1 AND status <> 'done' AND due_at IS NOT NULL AND due_at < $2
		ORDER BY due_at DESC
		LIMIT 10`

	ids, err := queryIDs(ctx, db, query, userID, now)
	if err != nil {
		return nil, fmt.Errorf("R-2 просроченные: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	return &Insight{
		Code:     "overdue_exists",
		Severity: SeverityWarning,
		Title:    fmt.Sprintf("%d %s просрочено", len(ids), pluralTasks(len(ids))),
		Message:  "Вам нужно вернуть эти задачи в работу или перенести дедлайн.",
		TaskIDs:  ids,
	}, nil
}

// R-3: due_soon_cluster — ≥3 задачи горят в ближайшие 24 часа.
func ruleDueSoonCluster(ctx context.Context, db *sql.DB, userID string, now time.Time) (*Insight, error) {
	const query = `
		SELECT id, title FROM tasks
		WHERE user_id = $1 AND status <> 'done' AND due_at IS NOT NULL
		  AND due_at >= $2 AND due_at < $3
		ORDER BY due_at
		LIMIT 10`

	rows, err := db.QueryContext(ctx, query, userID, now, now.Add(24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("R-3 горящие задачи: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 10)
	var firstTitle string
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("R-3 чтение строки: %w", err)
		}
		if firstTitle == "" {
			firstTitle = title
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("R-3 обход строк: %w", err)
	}
	if len(ids) < 3 {
		return nil, nil
	}

	return &Insight{
		Code:     "due_soon_cluster",
		Severity: SeverityWarning,
		Title:    fmt.Sprintf("%d %s горят в ближайшие 24 часа", len(ids), pluralTasks(len(ids))),
		Message: fmt.Sprintf("Суммарно на сегодня приходится %d %s — вероятно, план нереалистичен. Начните с «%s».",
			len(ids), pluralDeadlines(len(ids)), firstTitle),
		TaskIDs: ids,
	}, nil
}

// R-4: overloaded_day — ≥6 задач с дедлайном в один день.
func ruleOverloadedDay(ctx context.Context, db *sql.DB, userID string, _ time.Time) (*Insight, error) {
	const query = `
		SELECT date(due_at AT TIME ZONE 'UTC') AS day, count(*) AS total,
		       array_agg(id ORDER BY due_at) AS ids
		FROM tasks
		WHERE user_id = $1 AND status <> 'done' AND due_at IS NOT NULL
		GROUP BY 1
		HAVING count(*) >= 6
		ORDER BY day
		LIMIT 1`

	var day time.Time
	var total int
	var ids []string
	err := db.QueryRowContext(ctx, query, userID).Scan(&day, &total, &ids)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("R-4 перегруз по дням: %w", err)
	}

	label := formatDay(day)
	return &Insight{
		Code:     "overloaded_day",
		Severity: SeverityWarning,
		Title:    fmt.Sprintf("Перегруз на %s — %d %s", label, total, pluralTasks(total)),
		Message: fmt.Sprintf("На %s запланировано %d %s. Рассмотрите перенос части задач на другие дни.",
			label, total, pluralTasks(total)),
		TaskIDs: ids,
	}, nil
}

// R-5: no_deadline — ≥2 задачи в работе без срока.
func ruleNoDeadline(ctx context.Context, db *sql.DB, userID string, _ time.Time) (*Insight, error) {
	const query = `
		SELECT id, title FROM tasks
		WHERE user_id = $1 AND status = 'in_progress' AND due_at IS NULL
		ORDER BY created_at DESC
		LIMIT 10`

	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("R-5 задачи без срока: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 10)
	titles := make([]string, 0, 2)
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("R-5 чтение строки: %w", err)
		}
		ids = append(ids, id)
		if len(titles) < 2 {
			titles = append(titles, title)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("R-5 обход строк: %w", err)
	}
	if len(ids) < 2 {
		return nil, nil
	}

	return &Insight{
		Code:     "no_deadline",
		Severity: SeverityInfo,
		Title:    fmt.Sprintf("Задачи без срока: %d", len(ids)),
		Message: fmt.Sprintf("Задачи в работе без дедлайна могут висеть бесконечно. Установите срок для «%s» и «%s».",
			titles[0], titles[1]),
		TaskIDs: ids,
	}, nil
}

// R-6: stale_in_progress — задача в работе не менялась 3+ дней.
func ruleStaleInProgress(ctx context.Context, db *sql.DB, userID string, now time.Time) (*Insight, error) {
	const query = `
		SELECT id, title FROM tasks
		WHERE user_id = $1 AND status = 'in_progress' AND updated_at < $2
		ORDER BY updated_at
		LIMIT 5`

	rows, err := db.QueryContext(ctx, query, userID, now.Add(-3*24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("R-6 зависшие задачи: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 5)
	var firstTitle string
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, fmt.Errorf("R-6 чтение строки: %w", err)
		}
		if firstTitle == "" {
			firstTitle = title
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("R-6 обход строк: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	return &Insight{
		Code:     "stale_in_progress",
		Severity: SeverityInfo,
		Title:    fmt.Sprintf("Задачи без изменений 3+ дней: %d", len(ids)),
		Message: fmt.Sprintf("«%s» не менялась давно. Перенесите в «К работе» или завершите, если она уже не актуальна.",
			firstTitle),
		TaskIDs: ids,
	}, nil
}

// R-7: high_priority_in_todo — высокий приоритет, но работа не начата.
func ruleHighPriorityInTodo(ctx context.Context, db *sql.DB, userID string, now time.Time) (*Insight, error) {
	const query = `
		SELECT id, title, due_at FROM tasks
		WHERE user_id = $1 AND status = 'todo' AND priority = 'high'
		  AND due_at IS NOT NULL AND due_at <= $2
		ORDER BY due_at
		LIMIT 5`

	rows, err := db.QueryContext(ctx, query, userID, now.Add(48*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("R-7 высокий приоритет: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 5)
	var firstTitle string
	var firstDue time.Time
	for rows.Next() {
		var id, title string
		var due time.Time
		if err := rows.Scan(&id, &title, &due); err != nil {
			return nil, fmt.Errorf("R-7 чтение строки: %w", err)
		}
		if firstTitle == "" {
			firstTitle = title
			firstDue = due
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("R-7 обход строк: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	return &Insight{
		Code:     "high_priority_in_todo",
		Severity: SeverityInfo,
		Title:    fmt.Sprintf("Высокий приоритет, но ещё не начато: %d", len(ids)),
		Message: fmt.Sprintf("«%s» имеет высокий приоритет и дедлайн %s. Рассмотрите начало работы.",
			firstTitle, firstDue.UTC().Format("2 Jan 2006 15:04")),
		TaskIDs: ids,
	}, nil
}

// R-8: weekly_progress — за неделю завершено 2+ задач.
func ruleWeeklyProgress(ctx context.Context, db *sql.DB, userID string, now time.Time) (*Insight, error) {
	const query = `
		SELECT count(*) FROM tasks
		WHERE user_id = $1 AND status = 'done' AND updated_at >= $2`

	var total int
	if err := db.QueryRowContext(ctx, query, userID, now.Add(-7*24*time.Hour)).Scan(&total); err != nil {
		return nil, fmt.Errorf("R-8 прогресс за неделю: %w", err)
	}
	if total < 2 {
		return nil, nil
	}

	return &Insight{
		Code:     "weekly_progress",
		Severity: SeverityInfo,
		Title:    fmt.Sprintf("За неделю завершено: %d %s", total, pluralTasks(total)),
		Message:  "Хорошая динамика! Продолжайте в том же духе.",
		TaskIDs:  []string{},
	}, nil
}

func queryIDs(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0, 10)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// formatDay форматирует дату как «29 окт 2026» (docs/07-ai-insights.md §2, R-4).
func formatDay(day time.Time) string {
	months := [...]string{"янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
	return fmt.Sprintf("%d %s %d", day.Day(), months[int(day.Month())-1], day.Year())
}

// pluralTasks подбирает форму слова «задача» под число.
func pluralTasks(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "задача"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "задачи"
	default:
		return "задач"
	}
}

// pluralDeadlines подбирает форму слова «дедлайн» под число.
func pluralDeadlines(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "дедлайн"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "дедлайна"
	default:
		return "дедлайнов"
	}
}
