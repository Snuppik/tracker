package httpapi

import (
	"net/http"
	"strconv"
	"uuid"

	"tracker/internal/auth"
	"tracker/internal/model"
)

type notificationListResponse struct {
	Items  []model.Notification `json:"items"`
	Unread int                  `json:"unread"`
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	query := r.URL.Query()
	unreadOnly := query.Get("unread") == "true"

	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			failBadRequest(w, "limit", "ожидается целое число")
			return
		}
		limit = parsed
	}

	result, err := s.notifications.List(r.Context(), userID, unreadOnly, limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, notificationListResponse{Items: result.Items, Unread: result.Unread})
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	parsed, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		failBadRequest(w, "id", "ожидается UUID")
		return
	}

	if err := s.notifications.MarkRead(r.Context(), userID, parsed.String()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	if err := s.notifications.MarkAllRead(r.Context(), userID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
