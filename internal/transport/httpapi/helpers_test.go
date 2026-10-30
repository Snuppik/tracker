package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"tracker/db"
	"tracker/internal/auth"
	"tracker/internal/config"
	"tracker/internal/service"
	"tracker/internal/store"
	"tracker/internal/transport/httpapi"
)

// testDB — общая БД прогона. Тесты не параллелятся и чистят таблицы перед собой.
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

// resetDB удаляет данные только своих тестовых пользователей (@example.com).
// Пакеты гоняются параллельно и делят одну тестовую БД, поэтому общий TRUNCATE
// разрушил бы данные других пакетов прямо во время их тестов.
// Все дочерние таблицы (tasks, reminders, notifications, push_subscriptions)
// чистятся каскадно по users.id.
func resetDB(t *testing.T) {
	t.Helper()
	const query = `DELETE FROM users WHERE email LIKE '%@example.com'`
	if _, err := testDB.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("очистка тестовой БД: %v", err)
	}
}

// newTestServer поднимает транспортный слой на реальной тестовой БД.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	cfg := &config.Config{
		Env:          "dev",
		JWTExpiry:    time.Hour,
		CookieSecure: false,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := auth.NewTokenManager([]byte("test-secret-key"), time.Hour)

	server := httpapi.NewServer(
		cfg, logger, testDB, tokens,
		service.NewAuthService(store.NewUserStore(testDB), tokens),
		service.NewTaskService(testDB),
		service.NewReminderService(testDB),
		service.NewNotificationService(testDB),
		service.NewPushService(testDB, "test-vapid-public-key", true),
		service.NewStatsService(testDB),
	)

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// client — HTTP-клиент с cookie jar, имитирующий браузер.
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &client{t: t, base: ts.URL, http: &http.Client{Jar: jar, Timeout: 5 * time.Second}}
}

func (c *client) do(method, path string, body any) (*http.Response, []byte) {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal тела: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("создание запроса: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("чтение ответа: %v", err)
	}
	return resp, data
}

func (c *client) register(email, password, name string) map[string]any {
	c.t.Helper()
	resp, data := c.do(http.MethodPost, "/api/auth/register", map[string]string{
		"email": email, "password": password, "name": name,
	})
	if resp.StatusCode != http.StatusCreated {
		c.t.Fatalf("регистрация: код %d, тело %s", resp.StatusCode, data)
	}
	return decodeMap(c.t, data)
}

func (c *client) createTask(payload map[string]any) map[string]any {
	c.t.Helper()
	resp, data := c.do(http.MethodPost, "/api/tasks", payload)
	if resp.StatusCode != http.StatusCreated {
		c.t.Fatalf("создание задачи: код %d, тело %s", resp.StatusCode, data)
	}
	return decodeMap(c.t, data)
}

func decodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("разбор JSON %s: %v", data, err)
	}
	return m
}

func decodeItems(t *testing.T, data []byte) []any {
	t.Helper()
	m := decodeMap(t, data)
	items, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("в ответе нет items: %s", data)
	}
	return items
}

// hasErrorCode проверяет машинный код ошибки в контрактом формате.
func hasErrorCode(t *testing.T, data []byte, code string) bool {
	t.Helper()
	body := decodeMap(t, data)
	errBody, ok := body["error"].(map[string]any)
	if !ok {
		return false
	}
	got, _ := errBody["code"].(string)
	return got == code
}
