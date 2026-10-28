package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"tracker/internal/model"
)

// writeJSON пишет тело ответа с JSON и обязательным no-store для /api/*.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("запись JSON-ответа", "error", err)
	}
}

type errorDetail struct {
	Code    string             `json:"code"`
	Message string             `json:"message"`
	Details []model.FieldError `json:"details,omitempty"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

// writeError пишет ошибку в едином формате контракта (AGENTS.md §5).
func writeError(w http.ResponseWriter, status int, code, message string, details []model.FieldError) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message, Details: details}})
}
