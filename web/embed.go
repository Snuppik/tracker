// Package web содержит статику фронтенда, встроенную в бинарник (T-11).
package web

import "embed"

// Files — содержимое каталога web. Пути внутри FS — от корня каталога.
//
//go:embed all:*
var Files embed.FS
