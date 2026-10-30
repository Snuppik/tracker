// Package httpapi — транспортный слой: роутинг, middleware, хендлеры, формат ошибок.
package httpapi

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"tracker/internal/auth"
	"tracker/internal/config"
	"tracker/internal/insights"
	"tracker/internal/service"
)

// Server держит зависимости хендлеров. Никакого SQL и бизнес-правил здесь нет.
type Server struct {
	cfg           *config.Config
	log           *slog.Logger
	db            *sql.DB
	tokens        *auth.TokenManager
	auth          *service.AuthService
	tasks         *service.TaskService
	reminders     *service.ReminderService
	notifications *service.NotificationService
	push          *service.PushService
	stats         *service.StatsService
	insights      func(ctx context.Context, userID string) (*insights.List, error)
	static        http.Handler
}

// NewServer собирает транспортный слой.
func NewServer(cfg *config.Config, log *slog.Logger, db *sql.DB, tokens *auth.TokenManager,
	authSvc *service.AuthService, taskSvc *service.TaskService,
	reminderSvc *service.ReminderService, notificationSvc *service.NotificationService,
	pushSvc *service.PushService, statsSvc *service.StatsService) *Server {
	return &Server{
		cfg:           cfg,
		log:           log,
		db:            db,
		tokens:        tokens,
		auth:          authSvc,
		tasks:         taskSvc,
		reminders:     reminderSvc,
		notifications: notificationSvc,
		push:          pushSvc,
		stats:         statsSvc,
		insights: func(ctx context.Context, userID string) (*insights.List, error) {
			return insights.Collect(ctx, db, userID)
		},
		static: newStaticHandler(),
	}
}

// Handler возвращает готовый http.Handler со всеми middleware.
func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.recoverer(s.logRequests(s.routes())))
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Открытые маршруты.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)

	// Всё остальное под /api/ требует cookie tracker_session.
	protected := http.NewServeMux()
	protected.HandleFunc("POST /api/auth/logout", s.handleLogout)
	protected.HandleFunc("GET /api/auth/me", s.handleMe)

	protected.HandleFunc("GET /api/tasks", s.handleListTasks)
	protected.HandleFunc("POST /api/tasks", s.handleCreateTask)
	protected.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	protected.HandleFunc("PATCH /api/tasks/{id}", s.handlePatchTask)
	protected.HandleFunc("DELETE /api/tasks/{id}", s.handleDeleteTask)
	protected.HandleFunc("PUT /api/tasks/{id}/status", s.handleChangeStatus)

	protected.HandleFunc("GET /api/tasks/{id}/reminders", s.handleListReminders)
	protected.HandleFunc("POST /api/tasks/{id}/reminders", s.handleCreateReminder)
	protected.HandleFunc("DELETE /api/reminders/{id}", s.handleDeleteReminder)

	protected.HandleFunc("GET /api/notifications", s.handleListNotifications)
	protected.HandleFunc("POST /api/notifications/{id}/read", s.handleMarkRead)
	protected.HandleFunc("POST /api/notifications/read-all", s.handleMarkAllRead)

	protected.HandleFunc("GET /api/push/public-key", s.handleVapidKey)
	protected.HandleFunc("PUT /api/push/subscription", s.handlePutPushSubscription)
	protected.HandleFunc("DELETE /api/push/subscription", s.handleDeletePushSubscription)

	protected.HandleFunc("GET /api/stats", s.handleStats)
	protected.HandleFunc("GET /api/insights", s.handleInsights)

	mux.Handle("/api/", s.authRequired(protected))

	// Статика и SPA-fallback.
	mux.Handle("/", s.static)

	return mux
}
