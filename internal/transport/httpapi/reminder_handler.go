package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"tracker/internal/auth"
	"tracker/internal/model"
	"tracker/internal/service"
)

type reminderCreateRequest struct {
	FireAt  *string `json:"fire_at"`
	Message string  `json:"message"`
}

type reminderListResponse struct {
	Items []model.Reminder `json:"items"`
}

func (s *Server) handleListReminders(w http.ResponseWriter, r *http.Request) {
	userID, taskID, ok := s.taskTarget(w, r)
	if !ok {
		return
	}

	items, err := s.reminders.List(r.Context(), userID, taskID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reminderListResponse{Items: items})
}

func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	userID, taskID, ok := s.taskTarget(w, r)
	if !ok {
		return
	}

	var req reminderCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	if req.FireAt == nil || *req.FireAt == "" {
		failBadRequest(w, "fire_at", "обязательное поле")
		return
	}
	fireAt, err := time.Parse(time.RFC3339, *req.FireAt)
	if err != nil {
		failBadRequest(w, "fire_at", "неверный формат, ожидается RFC3339")
		return
	}

	reminder, err := s.reminders.Create(r.Context(), userID, taskID, service.ReminderCreateInput{
		FireAt:  fireAt,
		Message: req.Message,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, reminder)
}

func (s *Server) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
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

	if err := s.reminders.Delete(r.Context(), userID, parsed.String()); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
