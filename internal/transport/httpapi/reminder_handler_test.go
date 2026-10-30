package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// futureRFC3339 — валидный fire_at через d от «сейчас».
func futureRFC3339(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

func TestReminderCreateListDelete(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("rem@example.com", "secret123", "Напоминания")

	task := c.createTask(map[string]any{"title": "Сдать отчёт"})
	taskID := task["id"].(string)

	// Пустой список до создания.
	resp, data := c.do(http.MethodGet, "/api/tasks/"+taskID+"/reminders", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("список: код %d, тело %s", resp.StatusCode, data)
	}
	if items := decodeItems(t, data); len(items) != 0 {
		t.Errorf("до создания %d напоминаний, ожидалось 0", len(items))
	}

	fireAt := futureRFC3339(2 * time.Hour)
	resp, data = c.do(http.MethodPost, "/api/tasks/"+taskID+"/reminders",
		map[string]any{"fire_at": fireAt, "message": "Не забыть приложить график"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("создание: код %d, тело %s", resp.StatusCode, data)
	}
	created := decodeMap(t, data)
	if created["status"] != "pending" {
		t.Errorf("status = %v, ожидалось pending", created["status"])
	}
	if created["task_id"] != taskID {
		t.Errorf("task_id = %v, ожидалось %s", created["task_id"], taskID)
	}
	if created["message"] != "Не забыть приложить график" {
		t.Errorf("message = %v", created["message"])
	}
	// user_id не должен утекать в ответ (модель помечает его json:"-").
	if _, ok := created["user_id"]; ok {
		t.Errorf("в ответе есть user_id: %s", data)
	}
	reminderID, _ := created["id"].(string)
	if reminderID == "" {
		t.Fatalf("в ответе нет id: %s", data)
	}

	// Счётчик pending-напоминаний на задаче.
	resp, data = c.do(http.MethodGet, "/api/tasks/"+taskID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("деталь задачи: код %d, тело %s", resp.StatusCode, data)
	}
	if decodeMap(t, data)["reminder_count"] != float64(1) {
		t.Errorf("reminder_count = %v, ожидалось 1", decodeMap(t, data)["reminder_count"])
	}

	// В списке одно напоминание.
	resp, data = c.do(http.MethodGet, "/api/tasks/"+taskID+"/reminders", nil)
	if items := decodeItems(t, data); len(items) != 1 {
		t.Fatalf("в списке %d напоминаний, ожидалось 1: %s", len(items), data)
	}

	// Удаление.
	resp, data = c.do(http.MethodDelete, "/api/reminders/"+reminderID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("удаление: код %d, тело %s", resp.StatusCode, data)
	}

	resp, data = c.do(http.MethodGet, "/api/tasks/"+taskID+"/reminders", nil)
	if items := decodeItems(t, data); len(items) != 0 {
		t.Errorf("после удаления %d напоминаний, ожидалось 0: %s", len(items), data)
	}

	// Повторное удаление → 404.
	resp, _ = c.do(http.MethodDelete, "/api/reminders/"+reminderID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("повторное удаление: код %d, ожидался 404", resp.StatusCode)
	}
}

func TestReminderValidation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("remval@example.com", "secret123", "Валидация напоминаний")

	task := c.createTask(map[string]any{"title": "Задача"})
	taskID := task["id"].(string)
	path := "/api/tasks/" + taskID + "/reminders"

	cases := []struct {
		name    string
		payload map[string]any
		field   string
	}{
		{"без fire_at", map[string]any{"message": "есть текст"}, "fire_at"},
		{"пустой fire_at", map[string]any{"fire_at": ""}, "fire_at"},
		{"неверный формат", map[string]any{"fire_at": "29-10-2026 18:00"}, "fire_at"},
		{"далёкое прошлое", map[string]any{"fire_at": futureRFC3339(-2 * time.Hour)}, "fire_at"},
		{"слишком длинное сообщение",
			map[string]any{"fire_at": futureRFC3339(time.Hour), "message": strings.Repeat("а", 201)}, "message"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, data := c.do(http.MethodPost, path, tc.payload)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("код %d, ожидался 400, тело %s", resp.StatusCode, data)
			}
			if !hasField(t, data, tc.field) {
				t.Errorf("в details нет поля %q: %s", tc.field, data)
			}
		})
	}

	// Допуск в одну минуту на рассинхрон часов: fire_at на 30 секунд в прошлом — ок.
	resp, data := c.do(http.MethodPost, path, map[string]any{"fire_at": futureRFC3339(-30 * time.Second)})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("fire_at в пределах допуска: код %d, тело %s", resp.StatusCode, data)
	}

	// Невалидный UUID в пути → 400.
	resp, data = c.do(http.MethodPost, "/api/tasks/не-uuid/reminders", map[string]any{"fire_at": futureRFC3339(time.Hour)})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("битый id задачи: код %d, ожидался 400, тело %s", resp.StatusCode, data)
	}
}

func TestReminderLimitFivePerTask(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	c.register("remlim@example.com", "secret123", "Лимит напоминаний")

	task := c.createTask(map[string]any{"title": "Много напоминаний"})
	taskID := task["id"].(string)
	path := "/api/tasks/" + taskID + "/reminders"

	for i := 0; i < 5; i++ {
		resp, data := c.do(http.MethodPost, path,
			map[string]any{"fire_at": futureRFC3339(time.Duration(i+1) * time.Hour)})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("напоминание %d: код %d, тело %s", i+1, resp.StatusCode, data)
		}
	}

	// Шестое — 422 limit_exceeded.
	resp, data := c.do(http.MethodPost, path, map[string]any{"fire_at": futureRFC3339(6 * time.Hour)})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("шестое напоминание: код %d, ожидался 422, тело %s", resp.StatusCode, data)
	}
	if !hasErrorCode(t, data, "limit_exceeded") {
		t.Errorf("в ответе нет кода limit_exceeded: %s", data)
	}

	// Отправленные (не pending) лимит не занимают: удаляем pending и создаём снова.
	resp, data = c.do(http.MethodGet, path, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("список: код %d, тело %s", resp.StatusCode, data)
	}
	items := decodeItems(t, data)
	first, _ := items[0].(map[string]any)
	id, _ := first["id"].(string)
	if resp, _ = c.do(http.MethodDelete, "/api/reminders/"+id, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("удаление: код %d", resp.StatusCode)
	}

	resp, data = c.do(http.MethodPost, path, map[string]any{"fire_at": futureRFC3339(7 * time.Hour)})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("после удаления одного: код %d, ожидался 201, тело %s", resp.StatusCode, data)
	}
}

func TestReminderIsolation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	owner := newClient(t, ts)
	owner.register("remowner@example.com", "secret123", "Владелец")
	task := owner.createTask(map[string]any{"title": "Чужая задача"})
	taskID := task["id"].(string)

	resp, data := owner.do(http.MethodPost, "/api/tasks/"+taskID+"/reminders",
		map[string]any{"fire_at": futureRFC3339(time.Hour)})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("создание: код %d, тело %s", resp.StatusCode, data)
	}
	reminderID := decodeMap(t, data)["id"].(string)

	other := newClient(t, ts)
	other.register("remother@example.com", "secret123", "Чужой")

	paths := []struct {
		method string
		path   string
		body   map[string]any
	}{
		{http.MethodGet, "/api/tasks/" + taskID + "/reminders", nil},
		{http.MethodPost, "/api/tasks/" + taskID + "/reminders", map[string]any{"fire_at": futureRFC3339(time.Hour)}},
		{http.MethodDelete, "/api/reminders/" + reminderID, nil},
	}

	for _, tc := range paths {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			resp, data := other.do(tc.method, tc.path, tc.body)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("код %d, ожидался 404, тело %s", resp.StatusCode, data)
			}
		})
	}

	// Чужое напоминание всё ещё на месте.
	resp, data = owner.do(http.MethodGet, "/api/tasks/"+taskID+"/reminders", nil)
	if items := decodeItems(t, data); len(items) != 1 {
		t.Errorf("у владельца %d напоминаний, ожидалось 1: %s", len(items), data)
	}
}

func TestRemindersRequireAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, _ := c.do(http.MethodGet, "/api/tasks/019a0000-0000-7000-8000-000000000000/reminders", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET напоминаний без входа: код %d, ожидался 401", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodDelete, "/api/reminders/019a0000-0000-7000-8000-000000000000", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("DELETE напоминания без входа: код %d, ожидался 401", resp.StatusCode)
	}
}
