package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegisterSuccess(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, data := c.do(http.MethodPost, "/api/auth/register", map[string]string{
		"email": "Ivan@Example.COM", "password": "secret123", "name": "Иван",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("код %d, тело %s", resp.StatusCode, data)
	}

	body := decodeMap(t, data)
	user, ok := body["user"].(map[string]any)
	if !ok {
		t.Fatalf("нет объекта user: %s", data)
	}
	if user["email"] != "ivan@example.com" {
		t.Errorf("email не нормализован: %v", user["email"])
	}
	if _, exists := user["password_hash"]; exists {
		t.Error("password_hash попал в ответ")
	}

	cookie := findCookie(resp, "tracker_session")
	if cookie == nil {
		t.Fatal("cookie tracker_session не установлена")
	}
	if !cookie.HttpOnly {
		t.Error("cookie должна быть HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, ожидался Lax", cookie.SameSite)
	}
}

func TestRegisterValidation(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, data := c.do(http.MethodPost, "/api/auth/register", map[string]string{
		"email": "bad", "password": "123", "name": "",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", resp.StatusCode)
	}

	errBody, _ := decodeMap(t, data)["error"].(map[string]any)
	if errBody["code"] != "validation_failed" {
		t.Errorf("code = %v", errBody["code"])
	}
	details, _ := errBody["details"].([]any)
	if len(details) != 3 {
		t.Errorf("ожидалось 3 ошибки полей, получено %d: %s", len(details), data)
	}
}

func TestRegisterConflict(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	c.register("dup@example.com", "secret123", "Первый")

	resp, data := c.do(http.MethodPost, "/api/auth/register", map[string]string{
		"email": "DUP@example.com", "password": "secret123", "name": "Второй",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("код %d, ожидался 409, тело %s", resp.StatusCode, data)
	}
}

func TestLoginSuccessAndWrongPassword(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	c.register("login@example.com", "secret123", "Логин")

	resp, data := c.do(http.MethodPost, "/api/auth/login", map[string]string{
		"email": "login@example.com", "password": "secret123",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("вход: код %d, тело %s", resp.StatusCode, data)
	}
	if findCookie(resp, "tracker_session") == nil {
		t.Error("вход не установил cookie")
	}

	resp, _ = c.do(http.MethodPost, "/api/auth/login", map[string]string{
		"email": "login@example.com", "password": "wrongpass",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("неверный пароль: код %d, ожидался 401", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodPost, "/api/auth/login", map[string]string{
		"email": "absent@example.com", "password": "secret123",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("неизвестный email: код %d, ожидался 401", resp.StatusCode)
	}
}

func TestMeRequiresAuth(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, data := c.do(http.MethodGet, "/api/auth/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("код %d, ожидался 401", resp.StatusCode)
	}
	if !strings.Contains(string(data), "unauthorized") {
		t.Errorf("неожиданное тело: %s", data)
	}
}

func TestMeAndLogout(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	registered := c.register("me@example.com", "secret123", "Я")
	wantID := registered["user"].(map[string]any)["id"]

	resp, data := c.do(http.MethodGet, "/api/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: код %d, тело %s", resp.StatusCode, data)
	}
	user := decodeMap(t, data)["user"].(map[string]any)
	if user["id"] != wantID {
		t.Errorf("id = %v, ожидался %v", user["id"], wantID)
	}

	resp, _ = c.do(http.MethodPost, "/api/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: код %d, ожидался 204", resp.StatusCode)
	}

	resp, _ = c.do(http.MethodGet, "/api/auth/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("после logout me вернул %d, ожидался 401", resp.StatusCode)
	}
}

func TestBrokenTokenRejected(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "tracker_session", Value: "not-a-jwt"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("код %d, ожидался 401", resp.StatusCode)
	}
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
