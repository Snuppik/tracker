// Package model — доменные структуры и типизированные ошибки.
// Слой ничего не знает про HTTP и SQL.
package model

import "time"

// User — зарегистрированный пользователь. PasswordHash никогда не попадает в JSON.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}
