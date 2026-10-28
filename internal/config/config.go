// Package config читает и валидирует конфигурацию из переменных окружения.
// Локально подгружает .env (кроме ENV=production).
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config — все настройки процесса. Секреты в логи не выводятся.
type Config struct {
	Port            int
	DatabaseURL     string
	JWTSecret       []byte
	JWTExpiry       time.Duration
	Env             string
	CookieSecure    bool
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	ReminderTick    time.Duration
	ReminderGrace   time.Duration
	LogLevel        slog.Level
}

// Load читает окружение. Возвращает ошибку, если не заданы обязательные переменные.
func Load() (*Config, error) {
	env := getenv("ENV", "dev")
	if env != "production" {
		// Отсутствие .env — нормальный сценарий (Docker, CI).
		_ = godotenv.Load()
	}

	cfg := &Config{
		Port:            getenvInt("PORT", 8080),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		JWTSecret:       []byte(os.Getenv("JWT_SECRET")),
		JWTExpiry:       30 * 24 * time.Hour,
		Env:             env,
		VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:    getenv("VAPID_SUBJECT", "mailto:demo@tracker.local"),
		ReminderTick:    time.Duration(getenvInt("REMINDER_TICK_SECONDS", 30)) * time.Second,
		ReminderGrace:   time.Duration(getenvInt("REMINDER_GRACE_MINUTES", 10)) * time.Minute,
		LogLevel:        parseLevel(getenv("LOG_LEVEL", "info")),
	}

	cfg.CookieSecure = env == "production"
	if raw := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); raw != "" {
		secure, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("COOKIE_SECURE: %w", err)
		}
		cfg.CookieSecure = secure
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL не задан; пример: postgres://tracker:tracker@localhost:5432/tracker?sslmode=disable")
	}
	if len(cfg.JWTSecret) == 0 {
		return nil, fmt.Errorf("JWT_SECRET не задан; сгенерировать: openssl rand -hex 32")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("PORT вне диапазона 1..65535: %d", cfg.Port)
	}
	if cfg.ReminderTick <= 0 {
		return nil, fmt.Errorf("REMINDER_TICK_SECONDS должен быть больше нуля")
	}
	if cfg.ReminderGrace < 0 {
		return nil, fmt.Errorf("REMINDER_GRACE_MINUTES не может быть отрицательным")
	}

	return cfg, nil
}

// PushEnabled сообщает, настроены ли VAPID-ключи. Без них работает только in-app лента.
func (c *Config) PushEnabled() bool {
	return c.VAPIDPublicKey != "" && c.VAPIDPrivateKey != ""
}

// Addr — адрес HTTP-сервера.
func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
