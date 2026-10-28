package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"tracker/internal/auth"
	"tracker/internal/model"
	"tracker/internal/service"
)

type taskCreateRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	Status      string  `json:"status"`
	DueAt       *string `json:"due_at"`
}

// taskPatchRequest хранит due_at сырым JSON, чтобы отличить
// «поле не передано» (nil) от «очистить срок» (null).
type taskPatchRequest struct {
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Priority    *string         `json:"priority"`
	DueAt       json.RawMessage `json:"due_at"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type taskListResponse struct {
	Items  []model.Task `json:"items"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	query := r.URL.Query()
	limit, err := intParam(query.Get("limit"))
	if err != nil {
		failBadRequest(w, "limit", "ожидается целое число")
		return
	}
	offset, err := intParam(query.Get("offset"))
	if err != nil {
		failBadRequest(w, "offset", "ожидается целое число")
		return
	}

	result, err := s.tasks.List(r.Context(), userID, service.ListParams{
		Status:   query.Get("status"),
		Priority: query.Get("priority"),
		Search:   query.Get("search"),
		Sort:     query.Get("sort"),
		Dir:      query.Get("dir"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, taskListResponse{
		Items:  result.Items,
		Total:  result.Total,
		Limit:  result.Limit,
		Offset: result.Offset,
	})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return
	}

	var req taskCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	dueAt, err := parseOptionalTime(req.DueAt)
	if err != nil {
		failBadRequest(w, "due_at", "неверный формат, ожидается RFC3339")
		return
	}

	task, err := s.tasks.Create(r.Context(), userID, service.TaskCreateInput{
		Title:       req.Title,
		Description: req.Description,
		Priority:    model.Priority(req.Priority),
		Status:      model.Status(req.Status),
		DueAt:       dueAt,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	userID, taskID, ok := s.taskTarget(w, r)
	if !ok {
		return
	}

	task, err := s.tasks.Get(r.Context(), userID, taskID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handlePatchTask(w http.ResponseWriter, r *http.Request) {
	userID, taskID, ok := s.taskTarget(w, r)
	if !ok {
		return
	}

	var req taskPatchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	patch, err := buildPatch(req)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	task, err := s.tasks.Update(r.Context(), userID, taskID, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	userID, taskID, ok := s.taskTarget(w, r)
	if !ok {
		return
	}

	if err := s.tasks.Delete(r.Context(), userID, taskID); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangeStatus(w http.ResponseWriter, r *http.Request) {
	userID, taskID, ok := s.taskTarget(w, r)
	if !ok {
		return
	}

	var req statusRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "body", "некорректный JSON")
		return
	}

	task, err := s.tasks.ChangeStatus(r.Context(), userID, taskID, model.Status(req.Status))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// taskTarget достаёт user_id из контекста и проверяет UUID из пути.
func (s *Server) taskTarget(w http.ResponseWriter, r *http.Request) (userID, taskID string, ok bool) {
	userID, ok = auth.UserID(r.Context())
	if !ok {
		failUnauthorized(w)
		return "", "", false
	}

	parsed, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		failBadRequest(w, "id", "ожидается UUID")
		return "", "", false
	}
	return userID, parsed.String(), true
}

func buildPatch(req taskPatchRequest) (service.TaskPatch, error) {
	patch := service.TaskPatch{
		Title:       req.Title,
		Description: req.Description,
	}

	if req.Priority != nil {
		priority := model.Priority(*req.Priority)
		patch.Priority = &priority
	}

	if req.DueAt != nil {
		patch.DueAtSet = true
		if string(req.DueAt) != "null" {
			var raw string
			if err := json.Unmarshal(req.DueAt, &raw); err != nil {
				return patch, model.NewValidation("due_at", "неверный формат, ожидается RFC3339")
			}
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return patch, model.NewValidation("due_at", "неверный формат, ожидается RFC3339")
			}
			utc := parsed.UTC()
			patch.DueAt = &utc
		}
	}

	if patch.Title == nil && patch.Description == nil && patch.Priority == nil && !patch.DueAtSet {
		return patch, model.NewValidation("body", "передайте хотя бы одно поле")
	}
	return patch, nil
}

func parseOptionalTime(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	return &utc, nil
}

func intParam(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
