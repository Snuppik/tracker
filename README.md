# Tracker

Веб-приложение личного трекера задач. MVP, собранный за 4 дня командой из трёх человек для хакатона.

**Задача → срок → напоминание → пуш → аналитика.** Замкнутый цикл вместо ещё одного списка дел.

## Быстрый старт

```bash
cp .env.example .env
# заполнить JWT_SECRET (openssl rand -hex 32) и, опционально, VAPID-ключи (go run ./cmd/genvapid)

docker compose up -d --build      # http://localhost:8080
```

Проверка:

```bash
curl http://localhost:8080/healthz          # {"status":"ok"}
curl http://localhost:8080/readyz           # 200 — база доступна
```

Разработка (API локально, база в Docker):

```bash
docker compose up -d postgres
go run ./cmd/api                            # http://localhost:8080
make lint test                              # gofmt + vet + тесты
```

Требования: **Go 1.27**, Docker + Compose v2. Установка и план Б — в [docs/09-devops.md](docs/09-devops.md).

## Стек

| Слой | Технология | Почему |
|---|---|---|
| Язык | Go 1.27 | Требование команды. В stdlib: `net/http` с роутингом по методам, `log/slog`, `uuid`, `encoding/json/v2` |
| Роутинг | `http.ServeMux` (Go 1.22+) | 0 зависимостей, покрывает весь CRUD |
| БД | PostgreSQL 16 (Docker) | Одна живая база на троих |
| Доступ к БД | `database/sql` + `github.com/jackc/pgx/v5/stdlib` | Привычный интерфейс, pgx под капотом |
| Auth | JWT HS256 в cookie `tracker_session` (HttpOnly) | Токен недоступен JS → нет XSS-кражи, нет CORS |
| Уведомления | In-app лента + Web Push (VAPID) | Лента работает всегда, пуш — надстройка |
| Фронтенд | Vanilla HTML + CSS + ES-модули | Без сборщика, без фреймворков, без Node |
| Раздача статики | Тот же Go-процесс (`go:embed` + `http.FileServerFS`) | Один origin → CORS не нужен, `sw.js` получает scope `/` |
| Контейнеризация | `docker-compose.yml`: `postgres` + `api` | Один порт, запуск одной командой |

**5 прямых зависимостей в `go.mod`** — белый список зафиксирован в [AGENTS.md](AGENTS.md) §4.

## Документация

| Документ | Содержание |
|---|---|
| [AGENTS.md](AGENTS.md) | **Правила для AI-агентов.** Читать первым |
| [docs/00-decisions.md](docs/00-decisions.md) | Все продуктовые и технические решения + обоснования + принятые риски |
| [docs/01-product.md](docs/01-product.md) | Продукт, скоуп (must/should/won't), пользовательские истории, критерии приёмки |
| [docs/02-architecture.md](docs/02-architecture.md) | Слои, структура репозитория, потоки запросов, планировщик напоминаний, развёртывание |
| [docs/03-data-model.md](docs/03-data-model.md) | ER, полный DDL, индексы, эталонные SQL-запросы, правила схемы |
| [docs/04-api.md](docs/04-api.md) | REST-контракт: эндпоинты, DTO, коды ошибок |
| [api/openapi.yaml](api/openapi.yaml) | Машинно-читаемый контракт (источник истины) |
| [docs/05-frontend.md](docs/05-frontend.md) | Структура фронта, `api.js`, стор, drag&drop, дизайн-токены, XSS-правила |
| [docs/06-reminders-push.md](docs/06-reminders-push.md) | Напоминания, планировщик, Web Push, ограничения secure context, troubleshooting |
| [docs/07-ai-insights.md](docs/07-ai-insights.md) | 8 детерминированных правил подсказок |
| [docs/08-roles-plan.md](docs/08-roles-plan.md) | Задачи по участникам и дням, порядок жертв, чек-лист |
| [docs/09-devops.md](docs/09-devops.md) | Окружение, установка, env-переменные, Docker, Makefile, troubleshooting |
| [docs/10-demo-script.md](docs/10-demo-script.md) | Сценарий питча на 4 минуты, запасные варианты, ответы жюри |

## Структура репозитория

```
cmd/api/            точка входа: конфиг, БД, роутер, планировщик
cmd/genvapid/       генерация VAPID-ключей для Web Push
internal/
  config/           чтение env (+ .env), валидация обязательных переменных
  model/            доменные структуры, enum-константы, типизированные ошибки
  store/            SQL (users, tasks, reminders, notifications, push_subscriptions)
  service/          бизнес-правила, валидация, транзакции
  auth/             bcrypt, JWT, middleware
  insights/         rule-based движок подсказок
  push/             отправка Web Push (VAPID)
  scheduler/        фоновый тикер напоминаний
  transport/httpapi/ роутер, middleware, хендлеры, формат ошибок
db/schema.sql       идемпотентный DDL (go:embed, выполняется при старте)
web/                фронтенд: html, css, js, sw.js
api/openapi.yaml    контракт API
docs/               документация (см. таблицу выше)
```

## Что умеет

- Регистрация, вход, выход. Данные каждого пользователя изолированы (`user_id` из JWT в каждом SQL-запросе).
- Задачи: название, описание, приоритет (`low/medium/high`), срок, статус (`todo/in_progress/done`).
- Kanban-доска с drag&drop и список с фильтрами, поиском и сортировкой (состояние — в URL).
- Просроченные и «горящие» (≤24 ч) задачи подсвечиваются; флаги считает сервер.
- Напоминания к задаче (до 5 на задачу) → in-app лента + Web Push.
- Дашборд: активные / в работе / готовые за 7 дней / просроченные.
- Умные подсказки: 8 детерминированных правил (перегруз на день, зависшие задачи, cluster дедлайнов и т.д.).
- Работает офлайн: 0 внешних запросов из браузера.

## Чего здесь нет (намеренно)

Проекты и командная работа, комментарии, вложения, подзадачи, повторяющиеся задачи, календарь, экспорт, WebSocket/SSE, ORM, фреймворки, сборщики, LLM. Обоснование отказа — [docs/00-decisions.md](docs/00-decisions.md) §1 и §2.

## Команда

| Роль | Зона ответственности |
|---|---|
| Backend | `internal/`, `db/`, `api/`, Docker |
| Frontend-1 | `web/js/` — API-клиент, стор, kanban, напоминания |
| Frontend-2 | `web/css/`, `web/*.html` — дизайн-система, список, фильтры, дашборд |

Процесс: прямые пуши в `main`, обязательный зелёный `make lint test` перед пушем ([docs/00-decisions.md](docs/00-decisions.md) T-21).
