# 03. Модель данных

СУБД: PostgreSQL 16 (docker). Доступ: `database/sql` поверх `pgx/v5/stdlib`.
Схема: один файл `db/schema.sql`, идемпотентный, загружается через `go:embed` и выполняется в транзакции при старте API (`00-decisions.md` T-6).

## 1. ER-диаграмма

```
┌────────────┐        ┌─────────────────┐        ┌───────────────────┐
│ users      │1──────*│ tasks           │1──────*│ reminders         │
│------------│        │-----------------│        │-------------------│
│ id (PK)    │        │ id (PK)         │        │ id (PK)           │
│ email UQ   │        │ user_id (FK)    │        │ task_id (FK)      │
│ name       │        │ title           │        │ user_id (FK)      │
│ password_… │        │ description     │        │ fire_at           │
│ created_at │        │ status          │        │ message           │
└─────┬──────┘        │ priority        │        │ status            │
      │               │ due_at          │        │ attempts          │
      │1              │ created_at      │        │ sent_at           │
      │               │ updated_at      │        │ created_at        │
      │*              └────────┬────────┘        └───────────────────┘
┌─────┴──────────────┐         │1                 ┌───────────────────┐
│ push_subscriptions │*────────┘                  │ notifications     │
│--------------------│                            │-------------------│
│ id (PK)           │                            │ id (PK)           │
│ user_id (FK)      │────────────────────────────│ user_id (FK)      │
│ endpoint UQ       │        task_id (FK, NULL)  │ task_id           │
│ p256dh, auth      │                            │ kind              │
│ user_agent        │                            │ title, body       │
│ created_at        │                            │ read_at           │
│ last_seen_at      │                            │ created_at        │
└────────────────────┘                            └───────────────────┘
```

`user_id` в `reminders` денормализован (`task.user_id` → тот же пользователь, см. P-1/P-2). Это осознанно: планировщику не нужен JOIN к `tasks`, чтобы понять, кому слать, и скоуп «только свои» обеспечивается одним `WHERE`.

## 2. DDL (`db/schema.sql`)

```sql
-- Tracker schema. Идемпотентно: выполняется при каждом старте API.
-- Изменять только вместе с internal/model и api/openapi.yaml.

CREATE TABLE IF NOT EXISTS users (
    id             uuid PRIMARY KEY,
    email          text        NOT NULL UNIQUE,          -- хранится уже в lower()
    name           text        NOT NULL CHECK (name <> '' AND length(name) <= 100),
    password_hash  text        NOT NULL,                 -- bcrypt, cost 10
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title        text        NOT NULL CHECK (title <> '' AND length(title) <= 200),
    description  text        NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    status       text        NOT NULL DEFAULT 'todo'
                 CHECK (status IN ('todo', 'in_progress', 'done')),
    priority     text        NOT NULL DEFAULT 'medium'
                 CHECK (priority IN ('low', 'medium', 'high')),
    due_at       timestamptz,                            -- NULL = «без срока»
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Основные пути доступа: доска (user+status), список по срокам, «просроченные».
CREATE INDEX IF NOT EXISTS idx_tasks_user_status  ON tasks (user_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_user_due     ON tasks (user_id, due_at);
CREATE INDEX IF NOT EXISTS idx_tasks_user_created ON tasks (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS reminders (
    id           uuid PRIMARY KEY,
    task_id      uuid        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fire_at      timestamptz NOT NULL,
    message      text        NOT NULL DEFAULT '' CHECK (length(message) <= 200),
    status       text        NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'sent', 'expired', 'failed')),
    attempts     int         NOT NULL DEFAULT 0,
    sent_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Частичный индекс: планировщик читает только pending.
CREATE INDEX IF NOT EXISTS idx_reminders_due  ON reminders (fire_at)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_reminders_task ON reminders (task_id);
CREATE INDEX IF NOT EXISTS idx_reminders_user ON reminders (user_id, status);

CREATE TABLE IF NOT EXISTS notifications (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id      uuid        REFERENCES tasks(id) ON DELETE CASCADE,  -- NULL = системное
    kind         text        NOT NULL DEFAULT 'reminder'
                 CHECK (kind IN ('reminder', 'system')),
    title        text        NOT NULL,
    body         text        NOT NULL DEFAULT '',
    read_at      timestamptz,                            -- NULL = непрочитанное
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user
    ON notifications (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_unread
    ON notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint     text        NOT NULL UNIQUE,            -- один раз на подписку браузера
    p256dh       text        NOT NULL,
    auth         text        NOT NULL,
    user_agent   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_push_user ON push_subscriptions (user_id);
```

## 3. Правила модели

| Правило | Где обеспечивается | Почему так |
|---|---|---|
| ID создаёт приложение (`uuid.NewV7().String()`), не БД | `internal/model` + `service` | Единый способ для всех сущностей; v7 сортируется по времени — полезно для отладки и курсорной пагинации; дефолт `gen_random_uuid()` лишает нас этого и создаёт ID до валидации |
| Перечисления — `text` + `CHECK` | схема + `model` | `ALTER TYPE` в Postgres не позволяет удалить значение и требует `CONVERT`; CHECK меняется переопределением констрейнта |
| `email` нормализуется в `lower()` на уровне сервиса | `service/auth.go` | Уникальность без `citext`/расширений и без индекса по выражению |
| `updated_at` ставит приложение в `UPDATE` | `store/tasks.go` | Триггер на `now()` — «магия вне кода»; при отладке сложнее. В каждом `UPDATE ... SET ..., updated_at = now()` |
| Удаление задачи каскадно сносит напоминания и обрывает ссылки в уведомлениях | `ON DELETE CASCADE` | Иначе планировщик будет будить мёртвые напоминания |
| Просроченность — **вычисляемое**, не колонка | `service`/`insights` | `overdue` как поле устарело бы сразу после смены времени; считаем `due_at < now() AND status <> 'done'` |
| Один пользователь = один набор данных | `WHERE user_id = $1` в **каждом** запросе `store` | См. §5 |
| `due_at IS NULL` допустим | схема | Задача «когда-нибудь» — реальное использование; инсайт `no_deadline` ловит злоупотребление |

## 4. Ключевые запросы (эталонные формулировки)

**Список с фильтрами, поиском, сортировкой, пагинацией** (`store.ListTasks`):

```sql
SELECT id, title, description, status, priority, due_at, created_at, updated_at
FROM tasks
WHERE user_id = $1
  AND ($2::text IS NULL OR status   = $2)
  AND ($3::text IS NULL OR priority = $3)
  AND ($4::text IS NULL OR title ILIKE '%' || $4 || '%')
ORDER BY
  CASE WHEN $5 = 'due_at'    THEN COALESCE(due_at, 'infinity'::timestamptz) END  $6,
  CASE WHEN $5 = 'created_at' THEN created_at END                                 $6,
  CASE WHEN $5 = 'priority'
       THEN CASE priority WHEN 'high' THEN 3 WHEN 'medium' THEN 2 ELSE 1 END END  $6,
  created_at DESC
LIMIT  $7 OFFSET $8;
-- отдельно: SELECT count(*) FROM tasks WHERE <те же условия>  → total
```

`COALESCE(due_at, 'infinity')` — задачи без срока всегда в конце при сортировке по дедлайну. `$6` (`ASC`/`DESC`) **не подставляется в SQL из строки клиента**: маппится в Go на два заранее написанных варианта ORDER BY (см. §5 про инъекции).

**Планировщик забирает порцию к доставке:**

```sql
SELECT id, task_id, user_id, fire_at, message
FROM reminders
WHERE status = 'pending' AND fire_at <= $1
ORDER BY fire_at
LIMIT 50
FOR UPDATE SKIP LOCKED;   -- защита от двойной выборки, если запустят второй инстанс
```

**Статистика дашборда одним запросом:**

```sql
SELECT
  count(*) FILTER (WHERE status <> 'done')                                AS active,
  count(*) FILTER (WHERE status = 'in_progress')                          AS in_progress,
  count(*) FILTER (WHERE status = 'done' AND updated_at >= now() - 7*interval '1 day') AS done_7d,
  count(*) FILTER (WHERE status <> 'done' AND due_at IS NOT NULL
                   AND due_at < now())                                    AS overdue,
  count(*) FILTER (WHERE status <> 'done' AND due_at IS NOT NULL
                   AND due_at >= now() AND due_at < now() + interval '24 hours') AS due_24h
FROM tasks
WHERE user_id = $1;
```

**Инсайтам нужен групповой запрос** (см. `07-ai-insights.md`):

```sql
SELECT date(due_at AT TIME ZONE 'UTC') AS day, count(*),
       count(*) FILTER (WHERE status = 'done')
FROM tasks
WHERE user_id = $1 AND due_at IS NOT NULL
GROUP BY 1 ORDER BY 1;
```

## 5. Инъекции и скоуп — жёсткие требования к коду

1. **Только параметризованные запросы `$1, $2, …`.** Никакой конкатенации пользовательских строк в SQL, никогда. `fmt.Sprintf` в SQL допускается исключительно для выбора из **перечисленного в коде** фрагмента (ORDER BY, имя колонки из белого списка).
2. **Белый список для сортировки:** `sort ∈ {due_at, created_at, priority}`, `dir ∈ {asc, desc}`; всё остальное → `ErrValidation`.
3. **Каждый запрос к `tasks`/`reminders`/`notifications`/`push_subscriptions` содержит `user_id = $1`**, значение — из JWT-контекста. Проверяется ревьюем: в `internal/store` нет ни одного метода, принимающего `task_id` без `user_id`.
4. `LIMIT` жёстко ограничен: по умолчанию 50, максимум 200 (`min(limit, 200)`). Иначе один запрос с `limit=1000000` выносит память на демо.

## 6. Типы в Go (границы)

| Postgres | Go (в моделях) | JSON | Комментарий |
|---|---|---|---|
| `uuid` | `string` | `string` | `uuid.NewV7().String()`; `uuid.Parse` для валидации входящего ID из URL |
| `text` | `string` | `string` | |
| `timestamptz` | `time.Time` | `string` RFC3339 UTC | `NULL` → `*time.Time` (для `due_at`) |
| `int` | `int` | `number` | |
| `bool`/`NULL` флаги | `bool` | `boolean` | `read_at IS NOT NULL` → `read: bool` в DTO |

Правило: **nullable-колонка → указатель в Go** (`*time.Time`). Иначе «нулевая дата» `0001-01-01` уедет в JSON и фронт нарисует её пользователю — самый вероятный баг этого проекта.

Валидные строки времени на входе: `time.RFC3339` (`2026-10-29T18:30:00+03:00`). Фронт присылает значение `datetime-local` + смещение через `new Date(value).toISOString()`. Разбор — `time.Parse(time.RFC3339, s)`, при ошибке — `ErrValidation{Field:"due_at"}`.

## 7. Эволюция схемы (что делать, когда «надо новое поле»)

Порядок действий фиксирован, иначе у троих разъедется локальная БД:

1. Правка `db/schema.sql` **только additive**: `CREATE TABLE`/`ADD COLUMN ... DEFAULT`/`CREATE INDEX`.
2. Если поле не nullable и таблица не пуста — **только через `DEFAULT`**, иначе `ALTER` падает.
3. `make reset-db` (или `docker compose down -v && docker compose up -d postgres`) + `go run ./cmd/api` — схема пересоздаётся с нуля.
4. Синхронно обновить: `internal/model`, `internal/store`, DTO, `api/openapi.yaml`, `docs/04-api.md`.
5. В общий чат: «схема изменена, всем `docker compose down -v`».

`DROP TABLE`, `DROP COLUMN`, `RENAME` — запрещены без явного решения всей команды (см. `AGENTS.md` §7).
