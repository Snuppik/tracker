package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

// createTaskWith — создаёт задачу с датой срока, возвращает id.
func createTaskWith(c *client, title string, dueOffset time.Duration, extra map[string]any) string {
	c.t.Helper()
	payload := map[string]any{"title": title}
	if dueOffset != 0 {
		payload["due_at"] = futureRFC3339(dueOffset)
	}
	for k, v := range extra {
		payload[k] = v
	}
	return c.createTask(payload)["id"].(string)
}

func TestStatsCounters(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registerUser(c, "stats@example.com", "Статистика")

	// 1 просроченная, 2 в ближайшие 24 часа, 1 в работе без срока, 1 будет закрыта, 1 без срока в todo.
	createTaskWith(c, "Просрочена", -time.Hour, nil)
	createTaskWith(c, "Горит 1", 2*time.Hour, nil)
	createTaskWith(c, "Горит 2", 5*time.Hour, nil)
	createTaskWith(c, "В работе", 0, map[string]any{"status": "in_progress"})
	done := createTaskWith(c, "Сделано", 0, nil)

	resp, data := c.do(http.MethodPut, "/api/tasks/"+done+"/status", map[string]any{"status": "done"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("закрытие задачи: код %d, тело %s", resp.StatusCode, data)
	}

	resp, data = c.do(http.MethodGet, "/api/stats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: код %d, тело %s", resp.StatusCode, data)
	}
	stats := decodeMap(t, data)

	// Контракт: active, in_progress, done_7d, overdue, due_24h, total.
	want := map[string]float64{
		"active":      4, // все, кроме done
		"in_progress": 1,
		"done_7d":     1,
		"overdue":     1,
		"due_24h":     2,
		"total":       5,
	}
	for field, expected := range want {
		got, ok := stats[field].(float64)
		if !ok {
			t.Fatalf("в ответе нет поля %q: %s", field, data)
		}
		if got != expected {
			t.Errorf("%s = %v, ожидалось %v (весь ответ %s)", field, got, expected, data)
		}
	}
}

func TestStatsEmptyUser(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registerUser(c, "statsempty@example.com", "Пустая статистика")

	resp, data := c.do(http.MethodGet, "/api/stats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: код %d, тело %s", resp.StatusCode, data)
	}
	for _, field := range []string{"active", "in_progress", "done_7d", "overdue", "due_24h", "total"} {
		if got, _ := decodeMap(t, data)[field].(float64); got != 0 {
			t.Errorf("%s = %v, ожидалось 0", field, got)
		}
	}
}

func TestStatsIsolation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	owner := newClient(t, ts)
	registerUser(owner, "statsowner@example.com", "Владелец")
	createTaskWith(owner, "Моя просрочена", -time.Hour, nil)

	other := newClient(t, ts)
	registerUser(other, "otherstats@example.com", "Чужой")

	resp, data := other.do(http.MethodGet, "/api/stats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: код %d, тело %s", resp.StatusCode, data)
	}
	if total, _ := decodeMap(t, data)["total"].(float64); total != 0 {
		t.Errorf("total = %v, ожидалось 0: чужие задачи видны", total)
	}
}

func TestStatsRequireAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, _ := c.do(http.MethodGet, "/api/stats", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("stats без входа: код %d, ожидался 401", resp.StatusCode)
	}
}

func TestInsightsEndpoint(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registerUser(c, "insights@example.com", "Подсказки")

	// Без задач — только R-1.
	resp, data := c.do(http.MethodGet, "/api/insights", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("insights: код %d, тело %s", resp.StatusCode, data)
	}
	items := decodeItems(t, data)
	if len(items) != 1 {
		t.Fatalf("подсказок %d, ожидалось 1 (no_tasks): %s", len(items), data)
	}
	first := items[0].(map[string]any)
	if first["code"] != "no_tasks" || first["severity"] != "info" {
		t.Errorf("первая подсказка = %v, ожидалось no_tasks/info", first)
	}
	if _, ok := first["task_ids"].([]any); !ok {
		t.Errorf("task_ids отсутствует или не массив: %v", first["task_ids"])
	}
	if decodeMap(t, data)["generated_at"] == nil {
		t.Errorf("нет generated_at: %s", data)
	}

	// Просроченная задача добавляет R-2 и убирает R-1.
	createTaskWith(c, "Просрочена", -2*time.Hour, nil)

	resp, data = c.do(http.MethodGet, "/api/insights", nil)
	items = decodeItems(t, data)
	codes := map[string]bool{}
	for _, item := range items {
		m := item.(map[string]any)
		codes[m["code"].(string)] = true
	}
	if codes["no_tasks"] {
		t.Errorf("no_tasks осталась при существующей задаче: %s", data)
	}
	if !codes["overdue_exists"] {
		t.Errorf("нет overdue_exists: %s", data)
	}

	// Контракт: severity из enum, title ≤ 60, message ≤ 200.
	for _, item := range items {
		m := item.(map[string]any)
		switch m["severity"] {
		case "info", "warning", "critical":
		default:
			t.Errorf("недопустимый severity %v", m["severity"])
		}
		title, _ := m["title"].(string)
		if len([]rune(title)) > 60 {
			t.Errorf("title длиннее 60 символов: %q", title)
		}
		message, _ := m["message"].(string)
		if len([]rune(message)) > 200 {
			t.Errorf("message длиннее 200 символов: %q", message)
		}
	}
}

func TestInsightsMaxFive(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registerUser(c, "ins5@example.com", "Пять подсказок")

	// Набор, который трогает сразу много правил (R-2, R-4, R-5, R-7, R-8 = ровно 5).
	// 6 задач на один день в будущем (3 дня от сейчас) — R-4 overloaded.
	now := time.Now().UTC()
	baseDay := time.Date(now.AddDate(0, 0, 3).Year(), now.AddDate(0, 0, 3).Month(), now.AddDate(0, 0, 3).Day(),
		9, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		createTaskWith(c, "План на день", baseDay.Sub(now)+time.Duration(i)*time.Hour, nil)
	}
	createTaskWith(c, "Просрочена", -time.Hour, nil)
	createTaskWith(c, "В работе", 0, map[string]any{"status": "in_progress"})
	createTaskWith(c, "Тоже в работе", 0, map[string]any{"status": "in_progress"})
	createTaskWith(c, "Срочно и важно", time.Hour, map[string]any{"priority": "high"})
	doneA := createTaskWith(c, "Готово 1", 0, nil)
	doneB := createTaskWith(c, "Готово 2", 0, nil)
	for _, id := range []string{doneA, doneB} {
		c.do(http.MethodPut, "/api/tasks/"+id+"/status", map[string]any{"status": "done"})
	}

	resp, data := c.do(http.MethodGet, "/api/insights", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("insights: код %d, тело %s", resp.StatusCode, data)
	}
	if items := decodeItems(t, data); len(items) != 5 {
		t.Errorf("подсказок %d, ожидалось ровно 5 (лимит): %s", len(items), data)
	}
}

func TestInsightsScopedToUser(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	owner := newClient(t, ts)
	registerUser(owner, "insowner@example.com", "Владелец")
	createTaskWith(owner, "Чужая просрочена", -time.Hour, nil)

	other := newClient(t, ts)
	registerUser(other, "insother@example.com", "Чужой")

	resp, data := other.do(http.MethodGet, "/api/insights", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("insights: код %d, тело %s", resp.StatusCode, data)
	}
	items := decodeItems(t, data)
	if len(items) != 1 || items[0].(map[string]any)["code"] != "no_tasks" {
		t.Errorf("у чужого пользователя не no_tasks: %s", data)
	}
	for _, item := range items {
		m := item.(map[string]any)
		if ids, ok := m["task_ids"].([]any); ok && len(ids) > 0 {
			t.Errorf("подсказка ссылается на чужие задачи: %v", ids)
		}
	}
}

func TestInsightsRequireAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, _ := c.do(http.MethodGet, "/api/insights", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("insights без входа: код %d, ожидался 401", resp.StatusCode)
	}
}
