// Package db содержит встроенную схему БД (T-6: один идемпотентный DDL при старте).
package db

import _ "embed"

// Schema — содержимое db/schema.sql, попадает в бинарник через go:embed.
//
//go:embed schema.sql
var Schema string
