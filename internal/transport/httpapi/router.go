// Package httpapi — транспортный слой: роутинг, middleware, хендлеры, формат ошибок.
package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"

	"tracker/internal/auth"
	"tracker/internal/config"
	"tracker/internal/service"
)

// Server держит зависимости хендлеров. Никакого SQL и бизнес-правил здесь нет.
type Server struct {
	cfg    *config.Config
	log    *slog.Logger
	db     *sql.DB
	tokens *auth.TokenManager
	auth   *service.AuthService
	tasks  *service.TaskService
	static http.Handler
}

// NewServer собирает транспортный слой.
func NewServer(cfg *config.Config, log *slog.Logger, db *sql.DB,
	tokens *auth.TokenManager, authSvc *service.AuthService, taskSvc *service.TaskService) *Server {
	return &Server{
		cfg:    cfg,
		log:    log,
		db:     db,
		tokens: tokens,
		auth:   authSvc,
		tasks:  taskSvc,
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

	mux.Handle("/api/", s.authRequired(protected))

	// Статика и SPA-fallback.
	mux.Handle("/", s.static)

	return mux
}
