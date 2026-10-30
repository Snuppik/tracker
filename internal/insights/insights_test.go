package insights

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"tracker/db"
	"tracker/internal/store"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://tracker:tracker@localhost:5432/tracker_test?sslmode=disable"
	}

	ctx := context.Background()
	conn, err := store.Open(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "тестовая БД недоступна (%s): %v\n", dsn, err)
		os.Exit(1)
	}
	if err := store.Migrate(ctx, conn, db.Schema); err != nil {
		fmt.Fprintf(os.Stderr, "схема тестовой БД: %v\n", err)
		os.Exit(1)
	}

	testDB = conn
	code := m.Run()
	conn.Close()
	os.Exit(code)
}

// newTestUser создаёт пользователя и возвращает его id.
// Домен @insights.test отличается от домена других пакетов: пакеты тестов
// работают параллельно на одной БД и не должны мешать друг другу.
func newTestUser(t *testing.T) string {
	t.Helper()

	id := uuid.NewV7().String()
	_, err := testDB.ExecContext(context.Background(),
		`INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, 'Тест', 'hash')`,
		id, "insights-"+id+"@insights.test")
	if err != nil {
		t.Fatalf("вставка пользователя: %v", err)
	}

	t.Cleanup(func() {
		if _, err := testDB.ExecContext(context.Background(),
			`DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Errorf("удаление пользователя %s: %v", id, err)
		}
	})
	return id
}

// taskSpec — параметры задачи для вставки.
type taskSpec struct {
	status    string
	priority  string
	dueAt     time.Time // нулевое время = без срока
	updatedAt time.Time // нулевое время = now()
	title     string
}

// addTask вставляет задачу напрямую: инсайты проверяются на точных датах.
func addTask(t *testing.T, userID string, spec taskSpec) string {
	t.Helper()

	id := uuid.NewV7().String()
	status := spec.status
	if status == "" {
		status = "todo"
	}
	priority := spec.priority
	if priority == "" {
		priority = "medium"
	}
	title := spec.title
	if title == "" {
		title = "Задача " + id[:8]
	}
	updated := spec.updatedAt
	if updated.IsZero() {
		updated = time.Now().UTC()
	}

	var dueArg any
	if !spec.dueAt.IsZero() {
		dueArg = spec.dueAt.UTC()
	}

	_, err := testDB.ExecContext(context.Background(), `
		INSERT INTO tasks (id, user_id, title, status, priority, due_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		id, userID, title, status, priority, dueArg, updated)
	if err != nil {
		t.Fatalf("вставка задачи: %v", err)
	}
	return id
}

// findInsight возвращает подсказку с кодом и числом всех подсказок.
func findInsight(t *testing.T, userID, code string) (*Insight, int) {
	t.Helper()

	list, err := Collect(context.Background(), testDB, userID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if list.GeneratedAt.IsZero() {
		t.Errorf("generated_at пустой")
	}
	for i := range list.Items {
		if list.Items[i].Code == code {
			return &list.Items[i], len(list.Items)
		}
	}
	return nil, len(list.Items)
}

// mustInsight требует наличие подсказки с кодом.
func mustInsight(t *testing.T, userID, code string) *Insight {
	t.Helper()

	insight, _ := findInsight(t, userID, code)
	if insight == nil {
		t.Fatalf("нет подсказки %q для пользователя %s", code, userID)
	}
	return insight
}

func TestRuleNoTasks(t *testing.T) {
	userID := newTestUser(t)

	insight := mustInsight(t, userID, "no_tasks")
	if insight.Severity != SeverityInfo {
		t.Errorf("severity = %q, ожидалось info", insight.Severity)
	}
	if len(insight.TaskIDs) != 0 {
		t.Errorf("task_ids = %v, ожидалось пусто", insight.TaskIDs)
	}

	// С первой задачей подсказка исчезает.
	addTask(t, userID, taskSpec{title: "Первая"})
	if got, _ := findInsight(t, userID, "no_tasks"); got != nil {
		t.Errorf("no_tasks осталась после создания задачи")
	}
}

func TestRuleOverdueExists(t *testing.T) {
	userID := newTestUser(t)
	past := time.Now().UTC().Add(-time.Hour)

	first := addTask(t, userID, taskSpec{status: "todo", dueAt: past.Add(-time.Hour), title: "Просрочено 1"})
	second := addTask(t, userID, taskSpec{status: "in_progress", dueAt: past, title: "Просрочено 2"})

	insight := mustInsight(t, userID, "overdue_exists")
	if insight.Severity != SeverityWarning {
		t.Errorf("severity = %q, ожидалось warning", insight.Severity)
	}
	if len(insight.TaskIDs) != 2 {
		t.Errorf("task_ids = %v, ожидалось 2 задачи", insight.TaskIDs)
	}
	// Просроченные идут в порядке due_at DESC: самая свежая первой.
	if insight.TaskIDs[0] != second || insight.TaskIDs[1] != first {
		t.Errorf("порядок task_ids = %v, ожидалось [%s %s]", insight.TaskIDs, second, first)
	}

	// Завершённая задача не считается просроченной.
	done := addTask(t, userID, taskSpec{status: "done", dueAt: past})
	insight = mustInsight(t, userID, "overdue_exists")
	for _, id := range insight.TaskIDs {
		if id == done {
			t.Errorf("done-задача %s попала в overdue_exists", done)
		}
	}
}

func TestRuleDueSoonCluster(t *testing.T) {
	userID := newTestUser(t)
	now := time.Now().UTC()

	// Две задачи — порога в 3 нет.
	a := addTask(t, userID, taskSpec{dueAt: now.Add(2 * time.Hour), title: "Горит А"})
	b := addTask(t, userID, taskSpec{dueAt: now.Add(3 * time.Hour), title: "Горит Б"})
	if got, _ := findInsight(t, userID, "due_soon_cluster"); got != nil {
		t.Errorf("due_soon_cluster сработала на 2 задачах")
	}

	c := addTask(t, userID, taskSpec{dueAt: now.Add(4 * time.Hour), title: "Горит В"})
	insight := mustInsight(t, userID, "due_soon_cluster")
	if insight.Severity != SeverityWarning {
		t.Errorf("severity = %q, ожидалось warning", insight.Severity)
	}
	if len(insight.TaskIDs) != 3 {
		t.Errorf("task_ids = %v, ожидалось 3", insight.TaskIDs)
	}
	// Порядок — по возрастанию due_at.
	want := []string{a, b, c}
	for i, id := range insight.TaskIDs {
		if id != want[i] {
			t.Errorf("task_ids[%d] = %s, ожидалось %s", i, id, want[i])
		}
	}
	// В тексте упоминается ближайшая задача.
	if !contains(insight.Message, "Горит А") {
		t.Errorf("в message нет ссылки на ближайшую задачу: %q", insight.Message)
	}

	// Дедлайн дальше 24 часов не попадает в кластер.
	addTask(t, userID, taskSpec{dueAt: now.Add(30 * time.Hour), title: "Не сегодня"})
	insight = mustInsight(t, userID, "due_soon_cluster")
	if len(insight.TaskIDs) != 3 {
		t.Errorf("задача через 30 часов попала в кластер: %v", insight.TaskIDs)
	}
}

func TestRuleOverloadedDay(t *testing.T) {
	userID := newTestUser(t)

	// Шесть задач на один день в будущем — вне окна 24 часов, чтобы не трогать R-3.
	day := time.Now().UTC().Add(3 * 24 * time.Hour)
	day = time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		addTask(t, userID, taskSpec{dueAt: day.Add(time.Duration(i) * time.Hour)})
	}
	if got, _ := findInsight(t, userID, "overloaded_day"); got != nil {
		t.Errorf("overloaded_day сработала на 5 задачах")
	}

	addTask(t, userID, taskSpec{dueAt: day.Add(5 * time.Hour)})
	insight := mustInsight(t, userID, "overloaded_day")
	if insight.Severity != SeverityWarning {
		t.Errorf("severity = %q, ожидалось warning", insight.Severity)
	}
	if len(insight.TaskIDs) != 6 {
		t.Errorf("task_ids = %d, ожидалось 6", len(insight.TaskIDs))
	}
	if !contains(insight.Title, "окт") && !contains(insight.Title, "ноя") {
		t.Errorf("в title нет даты в формате «29 окт 2026»: %q", insight.Title)
	}
}

func TestRuleNoDeadline(t *testing.T) {
	userID := newTestUser(t)

	first := addTask(t, userID, taskSpec{status: "in_progress", title: "Без срока 1"})
	if got, _ := findInsight(t, userID, "no_deadline"); got != nil {
		t.Errorf("no_deadline сработала на 1 задаче")
	}

	second := addTask(t, userID, taskSpec{status: "in_progress", title: "Без срока 2"})
	insight := mustInsight(t, userID, "no_deadline")
	if insight.Severity != SeverityInfo {
		t.Errorf("severity = %q, ожидалось info", insight.Severity)
	}
	if len(insight.TaskIDs) != 2 {
		t.Errorf("task_ids = %v, ожидалось 2", insight.TaskIDs)
	}
	// Задача со сроком в in_progress не считается.
	addTask(t, userID, taskSpec{status: "in_progress", dueAt: time.Now().UTC().Add(time.Hour), title: "Со сроком"})
	insight = mustInsight(t, userID, "no_deadline")
	if len(insight.TaskIDs) != 2 {
		t.Errorf("задача со сроком попала в no_deadline: %v", insight.TaskIDs)
	}
	// Порядок — created_at DESC: вторая создана позже.
	if insight.TaskIDs[0] != second || insight.TaskIDs[1] != first {
		t.Errorf("порядок task_ids = %v, ожидалось [%s %s]", insight.TaskIDs, second, first)
	}
}

func TestRuleStaleInProgress(t *testing.T) {
	userID := newTestUser(t)

	fresh := addTask(t, userID, taskSpec{status: "in_progress", title: "Свежая в работе"})
	if got, _ := findInsight(t, userID, "stale_in_progress"); got != nil {
		t.Errorf("stale_in_progress сработала на свежей задаче")
	}

	stale := addTask(t, userID, taskSpec{
		status:    "in_progress",
		title:     "Зависшая",
		updatedAt: time.Now().UTC().Add(-4 * 24 * time.Hour),
	})
	insight := mustInsight(t, userID, "stale_in_progress")
	if insight.Severity != SeverityInfo {
		t.Errorf("severity = %q, ожидалось info", insight.Severity)
	}
	if len(insight.TaskIDs) != 1 || insight.TaskIDs[0] != stale {
		t.Errorf("task_ids = %v, ожидалось [%s]", insight.TaskIDs, stale)
	}
	if !contains(insight.Message, "Зависшая") {
		t.Errorf("в message нет названия зависшей задачи: %q", insight.Message)
	}

	// todo-задача без изменений — не «зависшая в работе».
	addTask(t, userID, taskSpec{
		status:    "todo",
		title:     "Вtodo надолго",
		updatedAt: time.Now().UTC().Add(-10 * 24 * time.Hour),
	})
	insight = mustInsight(t, userID, "stale_in_progress")
	for _, id := range insight.TaskIDs {
		if id == fresh {
			t.Errorf("свежая задача попала в stale_in_progress")
		}
	}
	if len(insight.TaskIDs) != 1 {
		t.Errorf("task_ids = %v, ожидалось 1 зависшая", insight.TaskIDs)
	}
}

func TestRuleHighPriorityInTodo(t *testing.T) {
	userID := newTestUser(t)
	now := time.Now().UTC()

	// Средний приоритет не триггерит.
	addTask(t, userID, taskSpec{status: "todo", priority: "medium", dueAt: now.Add(time.Hour)})
	if got, _ := findInsight(t, userID, "high_priority_in_todo"); got != nil {
		t.Errorf("high_priority_in_todo сработала на medium-приоритете")
	}

	// Высокий приоритет в todo с дедлайном через 24 часа.
	hot := addTask(t, userID, taskSpec{
		status: "todo", priority: "high", dueAt: now.Add(24 * time.Hour), title: "Важное",
	})
	insight := mustInsight(t, userID, "high_priority_in_todo")
	if insight.Severity != SeverityInfo {
		t.Errorf("severity = %q, ожидалось info", insight.Severity)
	}
	if len(insight.TaskIDs) != 1 || insight.TaskIDs[0] != hot {
		t.Errorf("task_ids = %v, ожидалось [%s]", insight.TaskIDs, hot)
	}

	// Дедлайн дальше 48 часов — подсказка не нужна.
	addTask(t, userID, taskSpec{status: "todo", priority: "high", dueAt: now.Add(72 * time.Hour), title: "Не срочно"})
	insight = mustInsight(t, userID, "high_priority_in_todo")
	if len(insight.TaskIDs) != 1 {
		t.Errorf("задача через 72 часа попала в high_priority_in_todo: %v", insight.TaskIDs)
	}

	// in_progress с высоким приоритетом — уже начата, не триггерит.
	addTask(t, userID, taskSpec{status: "in_progress", priority: "high", dueAt: now.Add(time.Hour), title: "В работе"})
	insight = mustInsight(t, userID, "high_priority_in_todo")
	if len(insight.TaskIDs) != 1 {
		t.Errorf("in_progress попала в high_priority_in_todo: %v", insight.TaskIDs)
	}
}

func TestRuleWeeklyProgress(t *testing.T) {
	userID := newTestUser(t)

	// Одна завершённая — мало для мотивации (docs/07-ai-insights.md §2, R-8).
	addTask(t, userID, taskSpec{status: "done", title: "Одна готово"})
	if got, _ := findInsight(t, userID, "weekly_progress"); got != nil {
		t.Errorf("weekly_progress сработала на 1 завершённой задаче")
	}

	addTask(t, userID, taskSpec{status: "done", title: "Вторая готово"})
	insight := mustInsight(t, userID, "weekly_progress")
	if insight.Severity != SeverityInfo {
		t.Errorf("severity = %q, ожидалось info", insight.Severity)
	}
	if len(insight.TaskIDs) != 0 {
		t.Errorf("task_ids = %v, ожидалось пусто", insight.TaskIDs)
	}

	// Завершённая 8 дней назад — вне окна.
	addTask(t, userID, taskSpec{
		status: "done", title: "Давно готово",
		updatedAt: time.Now().UTC().Add(-8 * 24 * time.Hour),
	})
	insight = mustInsight(t, userID, "weekly_progress")
	if !contains(insight.Title, "2") {
		t.Errorf("задача 8-дневной давности учтена: title = %q", insight.Title)
	}
}

// TestInsightsLimit проверяет отсечение в 5 подсказок и порядок R-1 → R-8.
func TestInsightsLimit(t *testing.T) {
	userID := newTestUser(t)
	now := time.Now().UTC()

	// Собираем данные, которые трогают максимум правил одновременно.
	day := now.Add(3 * 24 * time.Hour)
	day = time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		addTask(t, userID, taskSpec{dueAt: day.Add(time.Duration(i) * time.Hour)})
	}
	addTask(t, userID, taskSpec{dueAt: now.Add(-time.Hour)})               // R-2
	addTask(t, userID, taskSpec{status: "in_progress", title: "Зависшая"}) // R-5/R-6 контекст
	addTask(t, userID, taskSpec{status: "in_progress", updatedAt: now.Add(-4 * 24 * time.Hour)})
	addTask(t, userID, taskSpec{status: "todo", priority: "high", dueAt: now.Add(time.Hour)}) // R-7
	addTask(t, userID, taskSpec{status: "done"})                                              // R-8 (1 шт — не сработает)
	addTask(t, userID, taskSpec{status: "done"})

	list, err := Collect(context.Background(), testDB, userID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(list.Items) != maxItems {
		t.Fatalf("подсказок %d, ожидалось %d", len(list.Items), maxItems)
	}

	// Порядок правил: первая — overdue_exists (R-2), R-1 не срабатывает (задачи есть).
	if list.Items[0].Code != "overdue_exists" {
		t.Errorf("первая подсказка %q, ожидалось overdue_exists", list.Items[0].Code)
	}
	for i := 1; i < len(list.Items); i++ {
		if list.Items[i-1].Code == list.Items[i].Code {
			t.Errorf("дублируется код %q", list.Items[i].Code)
		}
	}
}

// TestInsightsScopedToUser проверяет, что чужие задачи не попадают в подсказки.
func TestInsightsScopedToUser(t *testing.T) {
	owner := newTestUser(t)
	other := newTestUser(t)

	past := time.Now().UTC().Add(-time.Hour)
	overdueID := addTask(t, owner, taskSpec{dueAt: past, title: "Чужая просроченная"})

	insight, count := findInsight(t, other, "overdue_exists")
	if insight != nil {
		t.Fatalf("у второго пользователя есть подсказка про чужую задачу: %v", insight.TaskIDs)
	}

	// У первого — есть, и она не пустая.
	list, err := Collect(context.Background(), testDB, owner)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var found bool
	for _, item := range list.Items {
		if item.Code == "overdue_exists" {
			found = true
			if len(item.TaskIDs) != 1 || item.TaskIDs[0] != overdueID {
				t.Errorf("task_ids = %v, ожидалось [%s]", item.TaskIDs, overdueID)
			}
		}
	}
	if !found {
		t.Errorf("у владельца нет overdue_exists, items = %d", count)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
