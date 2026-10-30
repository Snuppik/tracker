package httpapi_test

import (
	"net/http"
	"testing"
)

// TestHealthz — процесс жив и отвечает контрактным телом (04-api.md §8).
func TestHealthz(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, data := c.do(http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: код %d, тело %s", resp.StatusCode, data)
	}
	if decodeMap(t, data)["status"] != "ok" {
		t.Errorf("healthz вернул %s, ожидалось status=ok", data)
	}
}

// TestReadyz — БД доступна, значит 200 без тела.
func TestReadyz(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	resp, data := c.do(http.MethodGet, "/readyz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readyz: код %d, тело %s", resp.StatusCode, data)
	}
}

// TestSecurityHeaders — nosniff на всех ответах, no-store на API (AGENTS.md §6).
func TestSecurityHeaders(t *testing.T) {
	resetDB(t)
	ts := newTestServer(t)
	c := newClient(t, ts)

	for _, path := range []string{"/healthz", "/login", "/api/tasks"} {
		resp, _ := c.do(http.MethodGet, path, nil)
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, ожидалось nosniff", path, got)
		}
	}

	resp, _ := c.do(http.MethodGet, "/api/tasks", nil)
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("/api/tasks: Cache-Control = %q, ожидалось no-store", got)
	}
}
