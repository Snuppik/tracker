package model

import (
	"errors"
	"strings"
)

// Доменные ошибки. HTTP-коды проставляет только transport/httpapi/errors.go.
var (
	ErrNotFound      = errors.New("resource not found")
	ErrConflict      = errors.New("conflict")
	ErrLimitExceeded = errors.New("limit exceeded")
	ErrUnauthorized  = errors.New("unauthorized")
)

// FieldError — одна ошибка валидации конкретного поля.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError — набор ошибок валидации ввода (HTTP 400).
type ValidationError struct {
	Fields []FieldError
}

// NewValidation создаёт ошибку валидации для одного поля.
func NewValidation(field, message string) *ValidationError {
	return &ValidationError{Fields: []FieldError{{Field: field, Message: message}}}
}

// Add добавляет ошибку поля и возвращает получателя для цепочки вызовов.
func (e *ValidationError) Add(field, message string) *ValidationError {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: message})
	return e
}

// Empty сообщает, что ошибок нет.
func (e *ValidationError) Empty() bool {
	return len(e.Fields) == 0
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "validation failed"
	}
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+": "+f.Message)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// AsValidation извлекает *ValidationError из цепочки ошибок.
func AsValidation(err error) (*ValidationError, bool) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}
