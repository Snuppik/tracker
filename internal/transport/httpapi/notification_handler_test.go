package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
	"uuid"

	"tracker/internal/push"
	"tracker/internal/scheduler"
)

// registerUser регистрирует пользователя и возвращает его id.
func registerUser(c *client, email, name string) string {
	c.t.Helper()
	resp := c.register(email, "secret123", name)
	user, ok := resp["user"].(map[string]any)
	if !ok {
		c.t.Fatalf("в ответе регистрации нет user: %v", resp)
	}
	id, _ := user["id"].(string)
	if id == "" {
		c.t.Fatalf("в ответе регистрации нет user.id: %v", resp)
	}
	return id
}

// newScheduler поднимает планировщик без VAPID-ключей: пуш не отправляется,
// но in-app лента формируется как на проде.
func newScheduler(grace time.Duration) *scheduler.Scheduler {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return scheduler.New(testDB, log, push.New("", "", ""), time.Second, grace)
}

// insertDueReminder вставляет pending-напоминание с fire_at через offset от «сейчас»,
// минуя API-валидацию: планировщик должен реагировать и на просроченные записи.
func insertDueReminder(t *testing.T, userID, taskID string, offset time.Duration) string {
	t.Helper()
	id := uuid.NewV7().String()
	now := time.Now().UTC()
	_, err := testDB.ExecContext(context.Background(), `
		INSERT INTO reminders (id, task_id, user_id, fire_at, message, status, created_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6)`,
		id, taskID, userID, now.Add(offset), "Пора вернуться к задаче", now)
	if err != nil {
		t.Fatalf("вставка напоминания: %v", err)
	}
	return id
}

func reminderStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	err := testDB.QueryRowContext(context.Background(),
		`SELECT status FROM reminders WHERE id = $1`, id).Scan(&status)
	if err != nil {
		t.Fatalf("статус напоминания %s: %v", id, err)
	}
	return status
}

func TestSchedulerDeliversDueReminder(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	userID := registerUser(c, "sched@example.com", "Планировщик")

	task := c.createTask(map[string]any{"title": "Срочная задача"})
	taskID := task["id"].(string)
	reminderID := insertDueReminder(t, userID, taskID, -10*time.Second)

	newScheduler(time.Minute).ProcessOnce(context.Background())

	if got := reminderStatus(t, reminderID); got != "sent" {
		t.Fatalf("статус напоминания = %q, ожидалось sent", got)
	}

	resp, data := c.do(http.MethodGet, "/api/notifications", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("лента: код %d, тело %s", resp.StatusCode, data)
	}
	body := decodeMap(t, data)
	if body["unread"] != float64(1) {
		t.Errorf("unread = %v, ожидалось 1", body["unread"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("в ленте %d уведомлений, ожидалось 1: %s", len(items), data)
	}
	notif := items[0].(map[string]any)
	if notif["kind"] != "reminder" {
		t.Errorf("kind = %v, ожидалось reminder", notif["kind"])
	}
	if notif["title"] != "Срочная задача" {
		t.Errorf("title = %v, ожидалось название задачи", notif["title"])
	}
	if notif["body"] != "Пора вернуться к задаче" {
		t.Errorf("body = %v, ожидалось текст напоминания", notif["body"])
	}
	if notif["read"] != false {
		t.Errorf("read = %v, ожидалось false", notif["read"])
	}
	if notif["task_id"] != taskID {
		t.Errorf("task_id = %v, ожидалось %s", notif["task_id"], taskID)
	}

	// Второй проход не плодит дублей: напоминание уже не pending.
	newScheduler(time.Minute).ProcessOnce(context.Background())
	resp, data = c.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 1 {
		t.Errorf("после второго прохода %d уведомлений, ожидалось 1: %s", len(items), data)
	}
}

func TestSchedulerExpiresOutsideGraceWindow(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	userID := registerUser(c, "grace@example.com", "Grace window")

	task := c.createTask(map[string]any{"title": "Давняя задача"})
	reminderID := insertDueReminder(t, userID, task["id"].(string), -10*time.Minute)

	// Сервер лежал: напоминание созрело давно, в ленту оно уже не нужно.
	newScheduler(time.Minute).ProcessOnce(context.Background())

	if got := reminderStatus(t, reminderID); got != "expired" {
		t.Fatalf("статус = %q, ожидалось expired", got)
	}

	_, data := c.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 0 {
		t.Errorf("уведомлений быть не должно, есть %d: %s", len(items), data)
	}
}

func TestSchedulerSkipsDoneTask(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	userID := registerUser(c, "done@example.com", "Готовая задача")

	task := c.createTask(map[string]any{"title": "Уже не нужна"})
	taskID := task["id"].(string)

	resp, data := c.do(http.MethodPut, "/api/tasks/"+taskID+"/status", map[string]any{"status": "done"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("смена статуса: код %d, тело %s", resp.StatusCode, data)
	}
	// Напоминание вставляется после закрытия: планировщик всё равно должен его погасить.
	reminderID := insertDueReminder(t, userID, taskID, -10*time.Second)

	newScheduler(time.Minute).ProcessOnce(context.Background())

	if got := reminderStatus(t, reminderID); got != "expired" {
		t.Fatalf("статус = %q, ожидалось expired для done-задачи", got)
	}

	_, data = c.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 0 {
		t.Errorf("уведомлений по закрытой задаче быть не должно: %s", data)
	}
}

func TestSchedulerKeepsFutureReminder(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	userID := registerUser(c, "future@example.com", "Будущее напоминание")

	task := c.createTask(map[string]any{"title": "Не сейчас"})
	reminderID := insertDueReminder(t, userID, task["id"].(string), 2*time.Hour)

	newScheduler(time.Minute).ProcessOnce(context.Background())

	if got := reminderStatus(t, reminderID); got != "pending" {
		t.Fatalf("статус = %q, ожидалось pending", got)
	}

	_, data := c.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 0 {
		t.Errorf("лента должна быть пуста: %s", data)
	}
}

func TestSchedulerProcessesWholeBatch(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	userID := registerUser(c, "batch@example.com", "Пачка напоминаний")

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		task := c.createTask(map[string]any{"title": "Задача"})
		// Все fire_at внутри grace window (1 мин), чтобы планировщик доставил их в ленту.
		ids = append(ids, insertDueReminder(t, userID, task["id"].(string), -time.Duration(i+1)*10*time.Second))
	}

	newScheduler(time.Minute).ProcessOnce(context.Background())

	for _, id := range ids {
		if got := reminderStatus(t, id); got != "sent" {
			t.Errorf("напоминание %s в статусе %q, ожидалось sent", id, got)
		}
	}

	_, data := c.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 3 {
		t.Errorf("в ленте %d уведомлений, ожидалось 3: %s", len(items), data)
	}
}

func TestNotificationsMarkRead(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	userID := registerUser(c, "read@example.com", "Прочтение")

	for i := 0; i < 3; i++ {
		task := c.createTask(map[string]any{"title": "Задача"})
		insertDueReminder(t, userID, task["id"].(string), -10*time.Second)
	}
	newScheduler(time.Minute).ProcessOnce(context.Background())

	resp, data := c.do(http.MethodGet, "/api/notifications", nil)
	body := decodeMap(t, data)
	if body["unread"] != float64(3) {
		t.Fatalf("unread = %v, ожидалось 3: %s", body["unread"], data)
	}
	items := body["items"].([]any)
	first := items[0].(map[string]any)
	id, _ := first["id"].(string)

	resp, data = c.do(http.MethodPost, "/api/notifications/"+id+"/read", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mark read: код %d, тело %s", resp.StatusCode, data)
	}

	resp, data = c.do(http.MethodGet, "/api/notifications", nil)
	body = decodeMap(t, data)
	if body["unread"] != float64(2) {
		t.Errorf("unread = %v, ожидалось 2", body["unread"])
	}

	// Повторная отметка — не ошибка, счётчик не меняется.
	resp, _ = c.do(http.MethodPost, "/api/notifications/"+id+"/read", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("повторное mark read: код %d, ожидался 204", resp.StatusCode)
	}

	// Только непрочитанные.
	resp, data = c.do(http.MethodGet, "/api/notifications?unread=true", nil)
	if items := decodeItems(t, data); len(items) != 2 {
		t.Errorf("unread=true вернул %d, ожидалось 2: %s", len(items), data)
	}

	// read-all.
	resp, data = c.do(http.MethodPost, "/api/notifications/read-all", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("read-all: код %d, тело %s", resp.StatusCode, data)
	}
	resp, data = c.do(http.MethodGet, "/api/notifications", nil)
	body = decodeMap(t, data)
	if body["unread"] != float64(0) {
		t.Errorf("unread после read-all = %v, ожидалось 0", body["unread"])
	}
	if items := decodeItems(t, data); len(items) != 3 {
		t.Errorf("вся лента должна остаться, %d элементов", len(items))
	}

	// Несуществующее уведомление → 404.
	resp, _ = c.do(http.MethodPost, "/api/notifications/019a0000-0000-7000-8000-000000000000/read", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("чужое уведомление: код %d, ожидался 404", resp.StatusCode)
	}

	// Битый UUID → 400.
	resp, _ = c.do(http.MethodPost, "/api/notifications/abc/read", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("битый id: код %d, ожидался 400", resp.StatusCode)
	}
}

func TestNotificationsIsolation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	owner := newClient(t, ts)
	ownerID := registerUser(owner, "nowner@example.com", "Владелец")
	other := newClient(t, ts)
	registerUser(other, "nother@example.com", "Чужой")

	task := owner.createTask(map[string]any{"title": "Личная задача"})
	reminderID := insertDueReminder(t, ownerID, task["id"].(string), -10*time.Second)
	newScheduler(time.Minute).ProcessOnce(context.Background())

	resp, data := owner.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 1 {
		t.Fatalf("у владельца %d уведомлений, ожидалось 1: %s", len(items), data)
	}

	resp, data = other.do(http.MethodGet, "/api/notifications", nil)
	if items := decodeItems(t, data); len(items) != 0 {
		t.Errorf("чужой видит %d уведомлений: %s", len(items), data)
	}
	if body := decodeMap(t, data); body["unread"] != float64(0) {
		t.Errorf("unread чужого = %v, ожидалось 0", body["unread"])
	}

	// Чужой не может отметить чужое уведомление: id напоминания не является id уведомления,
	// но скоуп по user_id даёт 404 в любом случае.
	resp, data = other.do(http.MethodPost, "/api/notifications/"+reminderID+"/read", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("чужой mark read: код %d, ожидался 404, тело %s", resp.StatusCode, data)
	}
}

func TestNotificationsRequireAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/notifications"},
		{http.MethodPost, "/api/notifications/read-all"},
		{http.MethodPost, "/api/notifications/019a0000-0000-7000-8000-000000000000/read"},
	}
	for _, tc := range paths {
		resp, _ := c.do(tc.method, tc.path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: код %d, ожидался 401", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestPushSubscriptionLifecycle(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registerUser(c, "push@example.com", "Пуш")

	resp, data := c.do(http.MethodGet, "/api/push/public-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public-key: код %d, тело %s", resp.StatusCode, data)
	}
	if decodeMap(t, data)["public_key"] != "test-vapid-public-key" {
		t.Errorf("public_key = %v", decodeMap(t, data)["public_key"])
	}

	sub := map[string]any{
		"endpoint": "https://push.example.com/sub/1",
		"keys":     map[string]any{"p256dh": "key-p256dh", "auth": "key-auth"},
	}
	resp, data = c.do(http.MethodPut, "/api/push/subscription", sub)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("сохранение подписки: код %d, тело %s", resp.StatusCode, data)
	}

	// Идемпотентно по endpoint: повторная запись не конфликтует.
	resp, data = c.do(http.MethodPut, "/api/push/subscription", sub)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("повторное сохранение: код %d, ожидался 201, тело %s", resp.StatusCode, data)
	}

	var count int
	err := testDB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, sub["endpoint"]).Scan(&count)
	if err != nil {
		t.Fatalf("подсчёт подписок: %v", err)
	}
	if count != 1 {
		t.Errorf("в БД %d подписок, ожидалось 1", count)
	}

	resp, data = c.do(http.MethodDelete, "/api/push/subscription",
		map[string]any{"endpoint": sub["endpoint"]})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("удаление подписки: код %d, тело %s", resp.StatusCode, data)
	}

	err = testDB.QueryRowContext(context.Background(), `SELECT count(*) FROM push_subscriptions`).Scan(&count)
	if err != nil {
		t.Fatalf("подсчёт подписок: %v", err)
	}
	if count != 0 {
		t.Errorf("после удаления %d подписок, ожидалось 0", count)
	}
}

func TestPushSubscriptionIsolation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	owner := newClient(t, ts)
	registerUser(owner, "powner@example.com", "Владелец")
	other := newClient(t, ts)
	registerUser(other, "pother@example.com", "Чужой")

	endpoint := "https://push.example.com/shared"
	owner.do(http.MethodPut, "/api/push/subscription", map[string]any{
		"endpoint": endpoint,
		"keys":     map[string]any{"p256dh": "p", "auth": "a"},
	})

	// Чужое удаление того же endpoint: контракт даёт 204 как идемпотентную операцию,
	// но DELETE скоупен по user_id — подписка владельца остаётся на месте.
	resp, data := other.do(http.MethodDelete, "/api/push/subscription", map[string]any{"endpoint": endpoint})
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("чужое удаление: код %d, ожидался 204, тело %s", resp.StatusCode, data)
	}

	var count int
	err := testDB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM push_subscriptions WHERE endpoint = $1`, endpoint).Scan(&count)
	if err != nil {
		t.Fatalf("подсчёт: %v", err)
	}
	if count != 1 {
		t.Errorf("подписок %d, ожидалось 1 (владение по user_id)", count)
	}
}

func TestPushSubscriptionValidation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)
	registerUser(c, "pushval@example.com", "Валидация пуша")

	cases := []struct {
		name    string
		payload map[string]any
		field   string
	}{
		{"пустой endpoint", map[string]any{"endpoint": "  ", "keys": map[string]any{"p256dh": "p", "auth": "a"}}, "endpoint"},
		{"нет p256dh", map[string]any{"endpoint": "https://x", "keys": map[string]any{"auth": "a"}}, "keys.p256dh"},
		{"нет auth", map[string]any{"endpoint": "https://x", "keys": map[string]any{"p256dh": "p"}}, "keys.auth"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, data := c.do(http.MethodPut, "/api/push/subscription", tc.payload)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("код %d, ожидался 400, тело %s", resp.StatusCode, data)
			}
			if !hasField(t, data, tc.field) {
				t.Errorf("в details нет поля %q: %s", tc.field, data)
			}
		})
	}
}

func TestPushRequiresAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, _ := c.do(http.MethodGet, "/api/push/public-key", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("public-key без входа: код %d, ожидался 401", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodPut, "/api/push/subscription", map[string]any{
		"endpoint": "https://x", "keys": map[string]any{"p256dh": "p", "auth": "a"},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT subscription без входа: код %d, ожидался 401", resp.StatusCode)
	}
}
