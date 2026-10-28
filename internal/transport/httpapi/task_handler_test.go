package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"
	"uuid"
)

func TestCreateTaskSuccess(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("task@example.com", "secret123", "Задачи")

	// Относительные даты нужны по смыслу проверки overdue/due_soon.
	overdueAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	soonAt := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)

	overdue := c.createTask(map[string]any{"title": "Просроченная", "due_at": overdueAt})
	if overdue["overdue"] != true {
		t.Errorf("overdue = %v, ожидалось true", overdue["overdue"])
	}
	if overdue["status"] != "todo" || overdue["priority"] != "medium" {
		t.Errorf("дефолты не применились: %v", overdue)
	}

	soon := c.createTask(map[string]any{"title": "Горит", "priority": "high", "due_at": soonAt})
	if soon["due_soon"] != true {
		t.Errorf("due_soon = %v, ожидалось true", soon["due_soon"])
	}
	if soon["overdue"] != false {
		t.Errorf("overdue = %v, ожидалось false", soon["overdue"])
	}

	noDue := c.createTask(map[string]any{"title": "Без срока"})
	if noDue["due_at"] != nil {
		t.Errorf("due_at = %v, ожидался null", noDue["due_at"])
	}
	if noDue["reminder_count"] != float64(0) {
		t.Errorf("reminder_count = %v", noDue["reminder_count"])
	}
}

func TestCreateTaskValidation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("valid@example.com", "secret123", "Валидация")

	cases := []struct {
		name    string
		payload map[string]any
		field   string
	}{
		{"пустой title", map[string]any{"title": "   "}, "title"},
		{"неизвестный priority", map[string]any{"title": "Ok", "priority": "urgent"}, "priority"},
		{"неизвестный status", map[string]any{"title": "Ok", "status": "blocked"}, "status"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, data := c.do(http.MethodPost, "/api/tasks", tc.payload)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("код %d, ожидался 400, тело %s", resp.StatusCode, data)
			}
			if !hasField(t, data, tc.field) {
				t.Errorf("в details нет поля %q: %s", tc.field, data)
			}
		})
	}

	resp, data := c.do(http.MethodPost, "/api/tasks", map[string]any{
		"title": "Ok", "due_at": "28.10.2026",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("неверная дата: код %d, тело %s", resp.StatusCode, data)
	}
}

func TestListTasksFiltersAndPagination(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("list@example.com", "secret123", "Список")

	c.createTask(map[string]any{"title": "Сдать отчёт", "priority": "high"})
	c.createTask(map[string]any{"title": "Позвонить", "priority": "low"})
	c.createTask(map[string]any{"title": "Отчёт за квартал", "priority": "high", "status": "in_progress"})

	resp, data := c.do(http.MethodGet, "/api/tasks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d, тело %s", resp.StatusCode, data)
	}
	body := decodeMap(t, data)
	if body["total"] != float64(3) {
		t.Errorf("total = %v, ожидалось 3", body["total"])
	}

	_, data = c.do(http.MethodGet, "/api/tasks?priority=high", nil)
	if got := len(decodeItems(t, data)); got != 2 {
		t.Errorf("priority=high: %d задач, ожидалось 2", got)
	}

	_, data = c.do(http.MethodGet, "/api/tasks?status=in_progress", nil)
	if got := len(decodeItems(t, data)); got != 1 {
		t.Errorf("status=in_progress: %d задач, ожидалось 1", got)
	}

	_, data = c.do(http.MethodGet, "/api/tasks?search=отчёт", nil)
	if got := len(decodeItems(t, data)); got != 2 {
		t.Errorf("search=отчёт: %d задач, ожидалось 2", got)
	}

	_, data = c.do(http.MethodGet, "/api/tasks?limit=2&offset=0", nil)
	body = decodeMap(t, data)
	if len(decodeItems(t, data)) != 2 || body["limit"] != float64(2) || body["offset"] != float64(0) {
		t.Errorf("пагинация не применилась: %s", data)
	}
	if body["total"] != float64(3) {
		t.Errorf("total при пагинации = %v, ожидалось 3", body["total"])
	}

	_, data = c.do(http.MethodGet, "/api/tasks?limit=1000", nil)
	if got := decodeMap(t, data)["limit"]; got != float64(200) {
		t.Errorf("limit не ограничен: %v", got)
	}
}

func TestListTasksInvalidFilter(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("filter@example.com", "secret123", "Фильтр")

	for _, path := range []string{
		"/api/tasks?status=blocked",
		"/api/tasks?priority=urgent",
		"/api/tasks?sort=title",
		"/api/tasks?dir=up",
		"/api/tasks?limit=abc",
	} {
		resp, data := c.do(http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: код %d, ожидался 400, тело %s", path, resp.StatusCode, data)
		}
	}
}

func TestTaskIsolationBetweenUsers(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	alice := newClient(t, ts)
	alice.register("alice@example.com", "secret123", "Алиса")
	task := alice.createTask(map[string]any{"title": "Секрет Алисы"})
	taskID := task["id"].(string)

	bob := newClient(t, ts)
	bob.register("bob@example.com", "secret123", "Боб")

	_, data := bob.do(http.MethodGet, "/api/tasks", nil)
	if got := len(decodeItems(t, data)); got != 0 {
		t.Errorf("Боб видит %d чужих задач", got)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/tasks/" + taskID, nil},
		{http.MethodPatch, "/api/tasks/" + taskID, map[string]any{"title": "Взлом"}},
		{http.MethodDelete, "/api/tasks/" + taskID, nil},
		{http.MethodPut, "/api/tasks/" + taskID + "/status", map[string]any{"status": "done"}},
	} {
		resp, data := bob.do(tc.method, tc.path, tc.body)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: код %d, ожидался 404, тело %s", tc.method, tc.path, resp.StatusCode, data)
		}
	}

	resp, data := alice.do(http.MethodGet, "/api/tasks/"+taskID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("задача Алисы пропала: %d %s", resp.StatusCode, data)
	}
}

func TestPatchTaskPartial(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("patch@example.com", "secret123", "Патч")

	task := c.createTask(map[string]any{
		"title": "Старое имя", "description": "Описание", "priority": "low",
		"due_at": time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
	})
	taskID := task["id"].(string)

	resp, data := c.do(http.MethodPatch, "/api/tasks/"+taskID, map[string]any{"title": "Новое имя"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код %d, тело %s", resp.StatusCode, data)
	}
	patched := decodeMap(t, data)
	if patched["title"] != "Новое имя" {
		t.Errorf("title = %v", patched["title"])
	}
	if patched["description"] != "Описание" || patched["priority"] != "low" {
		t.Errorf("непереданные поля изменились: %v", patched)
	}

	resp, data = c.do(http.MethodPatch, "/api/tasks/"+taskID, map[string]any{"due_at": nil})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("очистка due_at: код %d, тело %s", resp.StatusCode, data)
	}
	if decodeMap(t, data)["due_at"] != nil {
		t.Errorf("due_at не очищен: %s", data)
	}

	resp, _ = c.do(http.MethodPatch, "/api/tasks/"+taskID, map[string]any{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("пустой патч: код %d, ожидался 400", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodPatch, "/api/tasks/"+taskID, map[string]any{"priority": "urgent"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("неверный priority: код %d, ожидался 400", resp.StatusCode)
	}
}

func TestChangeStatusCancelsReminders(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registered := c.register("status@example.com", "secret123", "Статус")
	userID := registered["user"].(map[string]any)["id"].(string)

	task := c.createTask(map[string]any{"title": "С напоминанием"})
	taskID := task["id"].(string)

	ctx := context.Background()
	_, err := testDB.ExecContext(ctx, `
		INSERT INTO reminders (id, task_id, user_id, fire_at, message, status, created_at)
		VALUES ($1, $2, $3, $4, 'скоро', 'pending', now())`,
		uuid.NewV7().String(), taskID, userID, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("вставка напоминания: %v", err)
	}

	resp, data := c.do(http.MethodGet, "/api/tasks/"+taskID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("деталь задачи: код %d, тело %s", resp.StatusCode, data)
	}
	detail := decodeMap(t, data)
	if detail["reminder_count"] != float64(1) {
		t.Errorf("reminder_count = %v, ожидалось 1", detail["reminder_count"])
	}
	if reminders, _ := detail["reminders"].([]any); len(reminders) != 1 {
		t.Errorf("в детали %d напоминаний, ожидалось 1", len(reminders))
	}

	resp, data = c.do(http.MethodPut, "/api/tasks/"+taskID+"/status", map[string]any{"status": "done"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("смена статуса: код %d, тело %s", resp.StatusCode, data)
	}
	if decodeMap(t, data)["status"] != "done" {
		t.Errorf("статус не изменился: %s", data)
	}

	var pending, expired int
	err = testDB.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'expired')
		 FROM reminders WHERE task_id = $1`, taskID).Scan(&pending, &expired)
	if err != nil {
		t.Fatalf("чтение напоминаний: %v", err)
	}
	if pending != 0 || expired != 1 {
		t.Errorf("pending=%d expired=%d, ожидалось 0 и 1", pending, expired)
	}
}

func TestDeleteTask(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("delete@example.com", "secret123", "Удаление")

	task := c.createTask(map[string]any{"title": "Удалить меня"})
	taskID := task["id"].(string)

	resp, _ := c.do(http.MethodDelete, "/api/tasks/"+taskID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("код %d, ожидался 204", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodGet, "/api/tasks/"+taskID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("после удаления код %d, ожидался 404", resp.StatusCode)
	}
}

func TestTasksRequireAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, _ := c.do(http.MethodGet, "/api/tasks", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/tasks: код %d, ожидался 401", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodPost, "/api/tasks", map[string]any{"title": "Без входа"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /api/tasks: код %d, ожидался 401", resp.StatusCode)
	}
}

func hasField(t *testing.T, data []byte, field string) bool {
	t.Helper()
	body := decodeMap(t, data)
	errBody, ok := body["error"].(map[string]any)
	if !ok {
		return false
	}
	details, ok := errBody["details"].([]any)
	if !ok {
		return false
	}
	for _, d := range details {
		if m, ok := d.(map[string]any); ok && m["field"] == field {
			return true
		}
	}
	return false
}
