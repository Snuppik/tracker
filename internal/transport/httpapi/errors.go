package httpapi

import (
	"errors"
	"net/http"

	"tracker/internal/model"
)

// Коды ошибок контракта (api/openapi.yaml, components.schemas.Error).
const (
	codeValidation   = "validation_failed"
	codeUnauthorized = "unauthorized"
	codeNotFound     = "not_found"
	codeConflict     = "conflict"
	codeLimit        = "limit_exceeded"
	codeInternal     = "internal"
)

// fail — единственное место, где доменная ошибка превращается в HTTP-код.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	if ve, ok := model.AsValidation(err); ok {
		writeError(w, http.StatusBadRequest, codeValidation, "Проверьте правильность заполнения полей", ve.Fields)
		return
	}

	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "Не найдено", nil)
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, codeConflict, "Конфликт: запись уже существует", nil)
	case errors.Is(err, model.ErrLimitExceeded):
		writeError(w, http.StatusUnprocessableEntity, codeLimit, "Превышен лимит", nil)
	case errors.Is(err, model.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "Требуется авторизация", nil)
	default:
		s.log.Error("внутренняя ошибка", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Внутренняя ошибка", nil)
	}
}

// failUnauthorized отвечает 401 без похода в сервис.
func failUnauthorized(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, codeUnauthorized, "Требуется авторизация", nil)
}

// failBadRequest отвечает 400 на некорректный JSON или параметр запроса.
func failBadRequest(w http.ResponseWriter, field, message string) {
	writeError(w, http.StatusBadRequest, codeValidation, "Проверьте правильность заполнения полей",
		[]model.FieldError{{Field: field, Message: message}})
}
