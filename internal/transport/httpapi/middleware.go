package httpapi

import (
	"net/http"
	"strings"
	"time"

	"tracker/internal/auth"
)

// sessionCookie — имя cookie с JWT (T-9).
const sessionCookie = "tracker_session"

// securityHeaders ставит обязательные заголовки: nosniff везде, no-store на /api/*.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// recoverer превращает panic в 500 и не рвёт соединение.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic в хендлере", "method", r.Method, "path", r.URL.Path, "panic", rec)
				writeError(w, http.StatusInternalServerError, codeInternal, "Внутренняя ошибка", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logRequests пишет строку slog на каждый запрос.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if rec.userID != "" {
			attrs = append(attrs, "user_id", rec.userID)
		}
		s.log.Info("http", attrs...)
	})
}

// authRequired читает cookie, проверяет JWT и кладёт user_id в контекст.
func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			failUnauthorized(w)
			return
		}

		userID, err := s.tokens.Parse(cookie.Value)
		if err != nil {
			failUnauthorized(w)
			return
		}

		if rec, ok := w.(*statusRecorder); ok {
			rec.userID = userID
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUserID(r.Context(), userID)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	userID string
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// setSessionCookie ставит HttpOnly-cookie с токеном.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.cfg.JWTExpiry.Seconds()),
	})
}

// clearSessionCookie сбрасывает cookie при выходе.
func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
