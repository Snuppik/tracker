# 02. Архитектура

## 1. Общая картина

Один Go-процесс, два слушателя в нём (HTTP-сервер и фоновый планировщик напоминаний), одна PostgreSQL.

```
┌──────────────────────────── Браузер (демо: localhost:8080) ───────────────────────────┐
│  login.html / index.html                                                              │
│  js: app.js → api.js → state.js → {kanban.js, list.js, dashboard.js, insights.js,     │
│                                     reminders.js, notifications.js}                   │
│  sw.js (Service Worker)  ← PushEventListener → new Notification(...)                  │
└───────┬───────────────────────────────────────────────────────────────────────────────┘
        │ fetch(..., {credentials:'same-origin'})  +  cookie: tracker_session (HttpOnly)
        ▼
┌──────────────────────── Go 1.27, binary `tracker` ──────────────────────┐
│                                                                          │
│  cmd/api/main.go                                                        │
│    ├ config.Load() (env + .env)                                         │
│    ├ store.Open(DATABASE_URL) → *sql.DB (pgx/v5/stdlib)                 │
│    ├ scheduler.Run(ctx, svc)      ← goroutine, ticker 30s               │
│    └ http.Server :8080                                                  │
│                                                                          │
│  internal/                                                              │
│    ├ transport/httpapi/   handlers, middleware, router, response, errors│
│    ├ service/             бизнес-правила + валидация                    │
│    ├ store/               SQL-запросы (tasks, users, reminders, ...)     │
│    ├ auth/                bcrypt, JWT, cookie                           │
│    ├── insights/          rule-based движок подсказок                   │
│    ├── push/              VAPID-обёртка над webpush-go                  │
│    └── model/             структуры домена + enums                      │
│                                                                          │
│  static: http.FileServerFS → /, /css, /js, /sw.js                       │
└───────────┬──────────────────────────────────┬───────────────────────────┘
            │ database/sql                     │ HTTPS POST
            ▼                                  ▼
   ┌──────────────────┐             ┌────────────────────────┐
   │ PostgreSQL 16     │             │ push service браузера   │
   │ (docker service)  │             │ (FCM / Mozilla / ...)   │
   └──────────────────┘             └─────────┬──────────────┘
                                              │ push event
                                              ▼
                                        Service Worker
```

**Ключевые свойства:** один origin (нет CORS), один процесс (нет очередей/Broker), один источник истины для уведомлений (таблица `notifications`), stateless API (курируемый только JWT в cookie → API можно перезапускать, но подписки/напоминания живут в БД).

## 2. Слои и правила зависимостей

Зависимости направлены строго «внутрь»: `httpapi → service → store`. Модель не знает ни о ком.

| Слой | Пакет | Обязанность | Чего НЕ делает |
|---|---|---|---|
| **model** | `internal/model` | Структуры (`Task`, `Reminder`, `Notification`, `User`, `PushSubscription`), константы статусов/приоритетов, `ValidationError` | Нет JSON-тегов для транспорта? — теги есть, но нет знания про HTTP |
| **store** | `internal/store` | Только SQL: `*sql.DB` → строки → структуры. Никаких бизнес-решений | Нет `if` про права пользователя, нет валидации ввода, нет `time.Now()` в решениях о бизнес-логике (кроме `created_at`) |
| **service** | `internal/service` | Правила: скоуп по `user_id`, валидация, лимиты (5 напоминаний), транзакции, отмена напоминаний при `done`, сборка статистики | Нет `http.Request`, нет `http.ResponseWriter`, нет SQL-строк |
| **httpapi** | `internal/transport/httpapi` | Декодирование JSON, коды ответов, заголовки cookie, middleware (auth, recover, logger), отдача статики | Нет бизнес-правил, нет SQL |
| **insights / push / auth** | отдельные пакеты | Инкапсулированные способности, вызываемые из `service` | Не знают про HTTP |

Правила, обязательные для соблюдения (зашиты и в `AGENTS.md`):
1. **Никакого SQL в `httpapi`.** Хендлер = декодировать → вызвать сервис → encoded result.
2. **Никакого `user_id` из тела запроса.** Идентификатор берётся только из контекста запроса, который положил auth-middleware (`middleware.UserID(r.Context())`). Это единственная защита от чтения чужих задач.
3. **Ошибки — типизированные.** `service` возвращает `model.ErrNotFound`, `model.ErrForbidden`, `model.ErrValidation{Fields: ...}`; `httpapi` мапит их в HTTP-код в одном месте (`errors.go`), а не по каждому хендлеру.
4. **Время — только `time.Time` с зоной UTC** и сравнения через `time.Now().UTC()`. Формат на границе — RFC3339.
5. Один хендлер = один метод одного сервиса. Если хендлер >40 строк — правило нарушено.

## 3. Структура репозитория

```
tracker/
├── AGENTS.md
├── README.md
├── docker-compose.yml
├── Dockerfile
├── .env.example
├── .gitignore
├── .gitattributes
├── go.mod                     // module tracker, go 1.27
├── cmd/
│   ├── api/main.go            // wiring + запуск HTTP и scheduler
│   └── genvapid/main.go       // генерация VAPID-ключа (запускать вручную, вывод в .env)
├── db/
│   └── schema.sql             // единый идемпотентный DDL (go:embed)
├── internal/
│   ├── config/config.go
│   ├── model/{task.go,user.go,reminder.go,push_subscription.go,errors.go}
│   ├── store/{db.go,users.go,tasks.go,reminders.go,notifications.go,push.go}
│   ├── service/{auth.go,tasks.go,stats.go,reminders.go,notifications.go}
│   ├── auth/{password.go,jwt.go,context.go}
│   ├── insights/insights.go
│   ├── push/sender.go
│   ├── scheduler/scheduler.go
│   └── transport/httpapi/
│       ├── router.go
│       ├── middleware.go
│       ├── response.go
│       ├── errors.go
│       ├── static.go
│       ├── system_handler.go
│       ├── auth_handler.go
│       ├── task_handler.go
│       ├── reminder_handler.go
│       ├── notification_handler.go
│       ├── push_handler.go
│       └── analytics_handler.go
├── web/                       // статика, отдаётся Go-процессом
│   ├── login.html
│   ├── index.html
│   ├── sw.js
│   ├── css/{tokens.css,base.css,components.css,pages.css}
│   └── js/{api.js,state.js,ui.js,kanban.js,list.js,dashboard.js,insights.js,reminders.js,notifications.js,task-modal.js,app.js}
├── api/openapi.yaml
└── docs/
```

`internal/` принципиально: подмодули не импортируются извне, и никто случайно не «вынесет общий код в библиотеку».

## 4. Поток №1 — типичный CRUD-запрос

```
fetch("GET /api/tasks?status=todo&priority=high&search=Отчёт")
  → http.Server
  → middleware: recover → requestLogger → authRequired (парсит cookie, валидирует JWT, кладёт user_id в ctx)
  → ServeMux матчит "GET /api/tasks"
  → taskHandler.List:
      парсит query (status/priority/search/sort/dir/limit/offset) в store.TaskFilter
      → taskService.List(ctx, userID, filter)
          → валидация фильтра (неизвестный status → model.ErrValidation)
          → store.ListTasks(ctx, userID, filter)  // SQL всегда содержит WHERE user_id = $1
          → []model.Task
      → response.WriteJSON(w, 200, TaskListDTO{items, total, limit, offset})
```

Запись/изменение — тот же путь, только тело JSON → `model.TaskInput` → `taskService.Update(ctx, userID, taskID, patch)`; внутри: `SELECT ... WHERE id=$1 AND user_id=$2` → `ErrNotFound`, если пусто (чужая задача неотличима от несуществующей — это намеренно, см. M-3), затем `UPDATE`.

## 5. Планировщик напоминаний (сердце фичи P-2)

Отдельная goroutine в том же процессе. Никакого cron-демона, Redis, очереди задач.

```go
// scheduler.Run(ctx) — запускается одной строкой из main.go
ticker := time.NewTicker(30 * time.Second)
defer ticker.Stop()
for {
    select {
    case <-ctx.Done():
        return                       // graceful shutdown
    case <-ticker.C:
        due, err := store.FetchDueReminders(ctx, time.Now().UTC(), batchSize=50)
        // 1) status='pending' AND fire_at <= now — выбираем по одному с FOR UPDATE SKIP LOCKED
        // 2) для каждого: создаём notification, после коммита отправляем push всем подпискам пользователя
        // 3) fire_at старше REMINDER_GRACE_MINUTES → status='expired' (не спамим после простоя)
        // 4) успех → status='sent', sent_at=now
    }
}
```

**Повторы не реализованы (осознанно, MVP).** Одна попытка на напоминание: in-app запись создаётся
в той же транзакции и не зависит от push, поэтому сбой push не влияет на доставку. `attempts` в схеме
остаётся для будущего retry, но код его не инкрементирует; `status='failed'` сейчас не выставляется.
Подписки, вернувшие `404/410`, удаляются, остальные ошибки логируются как `WARN`.

Три обязательных правила реализации (иначе баги, характерные именно для Go 1.27):

1. **`time.NewTicker`, а не `time.After` в цикле.** В Go 1.27 безвозвратно удалён `GODEBUG=asynctimerchan`: каналы, создаваемые пакетом `time`, **всегда синхронные (unbuffered)**. В `select` с `ticker.C` это безопасно, потому что получатель всегда готов; а вот `case <-time.After(d)` в горячем цикле накапливает горутины/аллокации. Плюс `ticker` требует `Stop()`.
2. **Одна транзакция на «напоминание → уведомление → смена статуса».** Иначе при падении между шагами получим отправленный пуш без записи в ленте или отправленное дважды. Отправка push выполняется **после** коммита записи (иллюзия атомарности сознательная компромисс: лучше одно лишнее уведомление в ленте, чем два пуша).
3. **`context.WithTimeout` на каждую внешнюю отправку** (5 с). Push-эндпоинты бывают мёртвыми; без таймаута горутинa висит и блокирует тик.

Обработка «приложение было выключено»: если сервер не работал 2 часа и `fire_at` прошёл, массовый залп из 40 уведомлений бесполезен. Правило: `now - fire_at > REMINDER_GRACE_MINUTES` (по умолчанию 10) → `status='expired'`, уведомление не создаётся, пуш не шлётся. Для демо это означает: **не откладывать показ на «задачу, созданную вчера»**.

## 6. Доставка уведомления: два уровня от одного события

```
reminder due
    └─→ INSERT notification            (источник истины, всегда)
            ├─→ in-app: фронт опрашивает GET /api/notifications?unread=true (30 с)
            └─→ Web Push: для каждой подписки пользователя → POST endpoint (VAPID)
```

In-app не зависит от того, работает ли push: колокольчик обновляется из той же таблицы. Поэтому при недоступном push-сервисе, на чужом ноутбуке или по `192.168.x.x` демо не ломается — деградирует до ленты. Детали и ограничения — `06-reminders-push.md`.

Почему опрос (polling), а не SSE/WebSocket: 4 дня, один пользователь на вкладку, допустимая задержка 30 с. Polling — ~10 строк на фронте и ноль состояния на бэке; SSE потребовал бы держать соединения, heartbeat, переподключение и усложнил бы перезапуск API во время демо.

## 7. Middleware и безопасность

| Middleware | Порядок | Что делает |
|---|---|---|
| `recover` | 1-й снаружи | `panic` → `500` + лог, соединение не рвётся |
| `requestLogger` | 2-й | `slog`: метод, путь, код, длительность, `user_id` (если есть) |
| `authRequired` | 3-й, только на `/api/*` кроме auth | читает cookie → парсит JWT → проверяет подпись/exp → `context.WithValue(ctx, ctxUserID, id)` |

Прочие обязательные меры:
- Cookie: `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` только при `ENV=production` (на `http://localhost` с `Secure` cookie не поставится — см. `09-devops.md`).
- Все запросы скоупятся по `user_id`; проверка «это моя задача» и «задача существует» объединена в один `WHERE` → отсутствие утечки существования чужих ID.
- Пароли: bcrypt, минимальная длина 8, максимум 72 байта (лимит bcrypt) — иначе `panic`/срезка.
- JWT: HS256, секрет из env, `exp` 30 дней. При отсутствии/битом секрете приложение **не стартует** (fail fast), а не генерирует случайный: иначе после рестарта все залогинены «никто».
- Заголовки: `X-Content-Type-Options: nosniff`, `Cache-Control: no-store` на `/api/*`.
- CORS не нужен (one origin). Не добавлять.

## 8. Развёртывание и запуск

Три сценария, все должны работать:

| Сценарий | Команда | Для чего |
|---|---|---|
| Полностью в Docker | `docker compose up -d --build` | Демо и «свежий клон». API + Postgres |
| БД в Docker, API локально | `docker compose up -d postgres && go run ./cmd/api` | Основной режим разработки: hot rebuild, логи в терминале, delve |
| Тесты | `docker compose up -d postgres && go test ./...` | Требует реальную БД (см. T-20) |

Схема `docker-compose.yml`:

```
services:
  postgres:
    image: postgres:16-alpine
    environment: POSTGRES_USER/PASSWORD/DB = tracker
    ports: "5432:5432"        // при конфликте локального PG → "5433:5432" и DATABASE_URL
    volumes: pgdata:/var/lib/postgresql/data
    healthcheck: pg_isready -U tracker
  api:
    build: .                  // multi-stage: golang:1.27-alpine → distroless/static
    depends_on: {postgres: {condition: service_healthy}}
    env_file: .env
    ports: "8080:8080"
```

`Dockerfile` — multi-stage: сборка на `golang:1.27-alpine` (`CGO_ENABLED=0`, `-ldflags="-s -w"`), релиз на `gcr.io/distroless/static-debian12`. Бинарник самодостаточен: статика, схема и всё остальное — через `go:embed`.

## 9. Архитектурные решения, которые мы НЕ приняли (чтобы никто не «улучшил»)

| Соблазн | Почему отказ |
|---|---|
| gRPC вместо REST | Браузер не поддерживает без прокси; см. T-3 |
| Redis для очереди напоминаний | Одна инстанс, один пользователь, задержка 30 с допустима. Ticker + `FOR UPDATE SKIP LOCKED` закрывают |
| ORM (GORM/sqlc) | sqlc — 1 день на настройку и кодогенерацию; GORM — абстракция ради абстракции при 15 запросах |
| Фреймворк (gin/echo/fiber) | `ServeMux` 1.22+ покрывает роутинг; фреймворк = ещё одна идеоматика для тех, кто пишет Go через агента |
| Микросервисы / отдельный notification-service | Один процесс дешевле отлаживать на демо |
| Vite/esbuild/Svelte | Требует Node, которого нет и который мы не ставим |
| WebSocket для живых уведомлений | См. §6 |
| Мультиязычность, темы в БД, настройки пользователя | Не входит в скоуп |

## 10. Точки интеграции между участниками

| Контракт | Кто поставляет | Кто потребляет | Замораживается |
|---|---|---|---|
| `api/openapi.yaml` | backend | FE1, FE2 | конец дня 1 |
| `db/schema.sql` | backend | все (для понимания полей) | конец дня 1 |
| `web/js/api.js` (все вызовы к бэку в одном файле) | FE1 | FE2 | конец дня 1 |
| `web/css/tokens.css` (все цвета/размеры/шрифты) | FE2 | FE1 | конец дня 1 |
| Формат `insights` (см. `07-ai-insights.md`) | backend | FE2 | день 2 |
| Формат push-подписки (`06-reminders-push.md` §5) | FE1 | backend | день 2 |

Правило: ни один из этих контрактов не меняется молча. Изменение = правка `api/openapi.yaml` (или `tokens.css`) **в том же коммите**, что и код, и сообщение в общий чат.
