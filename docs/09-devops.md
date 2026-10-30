# 09. Окружение, Docker, конфигурация

## 1. Требуемые версии

| Инструмент | Версия | Статус в текущем окружении | Что делать |
|---|---|---|---|
| Go | **1.27.x** | ❌ `go1.18.1 linux/amd64` | Установить (см. §2). Блокер: без 1.22+ нет роутинга `ServeMux` методом, без 1.21 нет `log/slog`, без 1.27 нет stdlib `uuid` |
| Docker Engine | ≥ 24 | ✅ `29.1.3` | — |
| Docker Compose v2 (plugin) | ≥ 2.20 | ❌ `docker compose version` → not found | Установить (см. §3). Без него команда `docker compose up` не существует |
| PostgreSQL | 16 (в контейнере) | ✅ клиент `psql 14.24` в системе — не мешает, сервер будет в контейнере | Не ставить PG в систему |
| Node.js | **не нужен** | ❌ отсутствует | Так и оставляем. Фронт без сборки (`00-decisions.md` T-12) |
| Git | ≥ 2.34 | ✅ `2.34.1` | Настроить `core.autocrlf=input` (см. §8) |
| ОС | Linux / WSL2 | ✅ Ubuntu 22.04 в WSL | — |

## 2. Установка Go 1.27 (WSL / Ubuntu)

Официальный способ — скачать tarball с go.dev (репозитории Ubuntu дают устаревшие версии):

```bash
# x86_64; для ARM заменить amd64 на arm64 (проверить: uname -m)
GOVER=1.27.0
wget -O /tmp/go.tgz "https://go.dev/dl/go${GOVER}.linux-amd64.tar.gz"
sha256sum /tmp/go.tgz            # сверить хеш со страницы https://go.dev/dl/
sudo rm -rf /usr/local/go        # удаляем старую установку Go, если была
sudo tar -C /usr/local -xzf /tmp/go.tgz

# в ~/.bashrc (или ~/.zshrc):
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
```

Проверка: `go version` → `go1.27.0 linux/amd64`. Если выводится 1.18 — в PATH раньше стоит дистрибутивный `/usr/bin/go`: проверить `which -a go` и убрать `golang-go` (`sudo apt remove golang-go`).

**Каждому участнику** (не только вам) — это не репозиторитная настройка. Проверить: `go version && docker compose version` должно даваться на всех трёх машинах **до** Дня 1.

## 3. Установка Docker Compose v2

```bash
# Вариант А (официальный репозиторий Docker — предпочтительно)
sudo apt-get update && sudo apt-get install -y docker-compose-plugin

# Вариант Б (если репозиторий недоступен — бинарник в cli-plugins)
mkdir -p ~/.docker/cli-plugins
COMPOSE_V=v2.32.4
curl -SL "https://github.com/docker/compose/releases/download/${COMPOSE_V}/docker-compose-linux-$(uname -m)" \
  -o ~/.docker/cli-plugins/docker-compose
chmod +x ~/.docker/cli-plugins/docker-compose

docker compose version    # должно напечатать v2.x
```

Проверка работы Docker в WSL: `docker ps` без sudo. Если требует sudo — добавить пользователя в группу: `sudo usermod -aG docker $USER` и перезапустить сессию WSL (`wsl --shutdown` из PowerShell).

## 4. План Б: без Docker (если у кого-то Docker не поднимется)

Только для разработки, не для демо:

```bash
# PostgreSQL из пакета
sudo apt-get install -y postgresql
sudo -u postgres createuser -s "$USER"; sudo -u postgres createdb -O "$USER" tracker
export DATABASE_URL="postgres://$USER@localhost/tracker?sslmode=disable"
go run ./cmd/api
```

Критерий: если к 12:00 Дня 1 у участника не работает `docker compose up -d postgres` — переходить на план Б и **не тратить на это день**.

## 5. Переменные окружения

Файл `.env` в корне, **не коммитится** (`.gitignore`), образец — `.env.example`.

| Переменная | Обязательная | Пример | Назначение |
|---|---|---|---|
| `PORT` | нет (default `8080`) | `8080` | HTTP-порт |
| `DATABASE_URL` | **да** | `postgres://tracker:tracker@postgres:5432/tracker?sslmode=disable` | Строка pgx. Внутри compose хост — `postgres`, снаружи — `localhost` |
| `JWT_SECRET` | **да** | `openssl rand -hex 32` | HS256. Без неё приложение **не стартует** (fail fast, см. `02-architecture.md` §7) |
| `ENV` | нет (default `dev`) | `dev` \| `production` | `production` включает `Secure` на cookie и JSON-логи |
| `VAPID_PUBLIC_KEY` | нет | base64url | Без неё push отключён, in-app работает (graceful) |
| `VAPID_PRIVATE_KEY` | нет | base64url | То же |
| `VAPID_SUBJECT` | нет | `mailto:demo@tracker.local` | Контакт для VAPID-аутентификации (заголовок `sub` в JWT-токене VAPID) |
| `REMINDER_TICK_SECONDS` | нет (default `30`) | `30` | Период планировщика |
| `REMINDER_GRACE_MINUTES` | нет (default `10`) | `10` | Окно «не спамить после простоя» |
| `LOG_LEVEL` | нет (default `info`) | `debug` \| `info` \| `warn` \| `error` | `slog` |

**Правила:**
1. `config.Load()` читает env; если `ENV=dev` и файл `.env` существует — подгружает его через `godotenv.Load()` (не переопределяя уже выставленные переменные окружения).
2. Отсутствие `DATABASE_URL` или `JWT_SECRET` → `log.Fatal` с внятным сообщением («JWT_SECRET not set; run: openssl rand -hex 32»). Не «генерируем случайный».
3. Отсутствие VAPID-ключей → `push.Enabled() == false`, планировщик создаёт уведомления, но не пытается слать пуш. Логируется один раз при старте: `web push disabled: VAPID keys not set`.
4. Секреты не выводятся в логи и не попадают в ответы API.

Генерация VAPID:

```bash
go run ./cmd/genvapid
# добавляет в stdout три строки для копирования в .env
```

## 6. `docker-compose.yml`

```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: tracker-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: tracker
      POSTGRES_PASSWORD: tracker
      POSTGRES_DB: tracker
    ports:
      - "5432:5432"        # конфликт с локальным PG → "5433:5432" и правка DATABASE_URL
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tracker -d tracker"]
      interval: 5s
      timeout: 3s
      retries: 10

  api:
    build: .
    container_name: tracker-api
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    env_file: .env
    environment:
      DATABASE_URL: postgres://tracker:tracker@postgres:5432/tracker?sslmode=disable
      ENV: production
    ports:
      - "8080:8080"

volumes:
  pgdata:
```

Нюансы, которые надо знать заранее:
- **`environment` перекрывает `env_file`** в compose: внутри контейнера БД всегда будет `@postgres`, даже если в `.env` стоит `@localhost`. Иначе на демо контейнер не найдёт базу.
- `depends_on` без `condition: service_healthy` не ждёт готовности — API стартует раньше PG и падает. Поэтому healthcheck обязателен.
- Хост-маппинг `5432` может конфликтовать с установленным в системе PostgreSQL 14. Проверка: `sudo ss -lntp | grep 5432`. Конфликт → `5433:5432` + `DATABASE_URL=...localhost:5433...` для локального запуска API.
- Внутри контейнера `ENV=production` включает `Secure` на cookie. **На демо через `http://localhost` это сломает авторизацию** (cookie не установится по HTTP). Решение: в compose оставить `ENV: dev`, либо добавить `COOKIE_SECURE=false` и использовать его как единственный источник истины. Рекомендуемый вариант — **`COOKIE_SECURE` отдельной переменной**, по умолчанию `false`; в `production` включать только за HTTPS.

## 7. `Dockerfile` (multi-stage)

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
# Сначала только go.mod/go.sum — слой не сбрасывается при правке кода
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Статика и схема попадают в бинарник через go:embed, поэтому COPY web/ и db/ обязателен
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tracker ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/tracker /app/tracker
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/tracker"]
```

Ожидания: размер образа ~25 МБ (без учёта базы), бинарник ~12 МБ. `CGO_ENABLED=0` обязателен — distroless не содержит libc.

## 8. Настройки git для кросс-платформенной команды

Если хотя бы один участник работает из Windows (а воркспейс на `/mnt/d` — значит работает):

`.gitattributes`:
```
* text=auto eol=lf
*.png binary
*.ico binary
```

`.editorconfig`:
```
root = true
[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = space
indent_size = 4
[*.js]
indent_size = 2
[*.css]
indent_size = 2
[*.html]
indent_size = 2
[*.md]
trim_trailing_whitespace = false
```

Команда один раз: `git config --global core.autocrlf input` (Windows: `false` + `.gitattributes`). Иначе `gofmt` будет показывать «изменённые» файлы у каждого, и диффы превратятся в шум.

**Рекомендация по расположению** (`00-decisions.md` T-23): клонировать в `~/projects/tracker` внутри WSL вместо `/mnt/d/tracker`. На `/mnt/d` (drvfs/9p) операции с файлами медленнее на порядок, `go build` это чувствует, плюс проблемы с правами и CRLF.

## 9. Рабочий процесс разработки

```bash
# 1. один раз: база
docker compose up -d postgres

# 2. запуск API локально (горячая переза сборкой, логи в терминале)
go run ./cmd/api                      # http://localhost:8080

# 3. быстрая проверка
go build ./... && go vet ./... && gofmt -l . && go test ./...

# 4. полная сборка в контейнере (перед демо!)
docker compose up -d --build
```

Полезные команды:

```bash
```bash
make reset-db          # docker compose down -v && docker compose up -d postgres
make test-db           # создать пустую БД tracker_test (один раз; тесты её не удаляют)
make run               # go run ./cmd/api
make test              # go test ./... -count=1
make lint              # gofmt -l . && go vet ./...
make cover             # go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
make logs              # docker compose logs -f api
make psql              # docker compose exec postgres psql -U tracker -d tracker
```

Тесты ходят в отдельную БД `tracker_test` (`TEST_DATABASE_URL`, по умолчанию
`postgres://tracker:tracker@localhost:5432/tracker_test?sslmode=disable`) и применяют к ней
`db/schema.sql` при старте. Пакеты тестов выполняются параллельно на одной БД, поэтому каждый
пакет чистит только свои данные (пользователи с собственным доменом email), а не делает `TRUNCATE`.
```

Отладка БД:

```bash
docker compose exec postgres psql -U tracker -d tracker
tracker=# SELECT status, count(*) FROM tasks GROUP BY status;
tracker=# SELECT id, fire_at, status, attempts FROM reminders ORDER BY fire_at DESC LIMIT 10;
```

## 10. Makefile (эталон)

```makefile
.PHONY: run build test lint cover reset-db test-db logs psql genvapid demo

run:        ; go run ./cmd/api
build:      ; CGO_ENABLED=0 go build -o bin/tracker ./cmd/api
test:       ; go test ./... -count=1
lint:       ; test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1); go vet ./...
cover:      ; go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
reset-db:   ; docker compose down -v && docker compose up -d postgres
test-db:    ; docker compose exec -T postgres psql -U tracker -d tracker -c "CREATE DATABASE tracker_test" || true
logs:       ; docker compose logs -f api
psql:       ; docker compose exec postgres psql -U tracker -d tracker
genvapid:   ; go run ./cmd/genvapid
demo:       ; docker compose down -v && docker compose up -d --build
```

`make demo` — то, что нужно прогнать **утром в день хакатона** (с нуля, с удалённым томом: проверяет, что приложение не зависит от состояния локальной БД).

## 11. Troubleshooting окружения

| Симптом | Причина | Решение |
|---|---|---|
| `unknown pattern syntax "GET /api/tasks"` | Go 1.18 (роутинг по методу появился в 1.22) | Обновить Go, проверить `go version` и `which -a go` |
| `undefined: uuid` при импорте `uuid` | Go < 1.27 (stdlib-пакет появился в 1.27) | Обновить Go; либо временно `github.com/google/uuid` (и зафиксировать это в `00-decisions.md`) |
| `log/slog` не найден | Go < 1.21 | Обновить Go |
| `docker compose` — «not a docker command» | Не установлен v2 plugin | §3 |
| `bind: address already in use` на 5432 | Системный PostgreSQL 14 слушает порт | `5433:5432` в compose + правка `DATABASE_URL`, либо `sudo systemctl stop postgresql` |
| API падает: `dial tcp ... connection refused` | База ещё не поднялась | Проверить `depends_on.condition` и healthcheck; вручную — `docker compose ps` |
| После логина сразу 401 | `Secure` cookie на `http://localhost` | `COOKIE_SECURE=false` / `ENV=dev` |
| `permission denied` на docker | Пользователь не в группе docker | `sudo usermod -aG docker $USER` + перезапуск WSL |
| Сборка «висит» на `/mnt/d` | drvfs metadata overhead | Перенести репозиторий в `~/projects/tracker` |
| `go: module requires go >= 1.27` | В `go.mod` стоит `go 1.27`, а участник на 1.26 | Обновить Go; временное решение — `GOTOOLCHAIN=auto` (скачает тулчейн сам) |
| Пуш не подписывается | Не secure context (IP вместо localhost) | Открыть `http://localhost:8080`, см. `06-reminders-push.md` §5 |

## 12. Что показать комиссии из «инженерной кухни»

- `docker compose up -d --build` с нуля (с `make demo`, удалив том) → приложение работает через ~40 секунд. Это самый убедительный слайд про культуру разработки.
- `go test ./...` — зелёный вывод в конце.
- `go mod graph | wc -l` — маленькое дерево зависимостей (5 прямых зависимостей, см. `AGENTS.md` §4).
- Отсутствие Node_modules: «фронт на нативных API браузера, 0 внешних запросов, работает офлайн».
