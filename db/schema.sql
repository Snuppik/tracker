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
