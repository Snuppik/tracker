# 08. Роли и план по дням

## 1. Распределение

| Участник | Роль | Ответственность |
|---|---|---|
| Backend | 1 человек | Схема БД, миграции, auth, CRUD задач, фильтры, статистика, напоминания (CRUD + планировщик + push), insights, тесты, Dockerfile |
| Frontend-1 | 1 человек | `api.js` (все запросы), `state.js`, kanban (drag&drop), task-modal, reminders-ui, оптимистичные обновления, polling ленты |
| Frontend-2 | 1 человек | Дизайн-токены (`tokens.css`), layout, список, фильтры+поиск, дашборд, логин, колокольчик+лента, тёмная тема, модалки, empty states |

Почему именно так:
- Backend — единственный, кто пишет Go. Он отвечает за **всё бэкенд-ядро**, включая push и планировщик.
- FE1 берёт логику работы с данными (api, state, kanban, DnD, reminders). Это ближе к бэку и требует понимания JSON-контракта.
- FE2 берёт визуал и состояние (layout, список, фильтры в URL, дашборд, уведомления UI, дизайн-система). Это ближе к дизайну и пользовательскому опыту.

## 2. План по дням (4 дня)

### День 1: scaffold + ядро (утро — инфраструктура, вечер — CRUD)

| Backend | FE1 | FE2 |
|---|---|---|
| `go mod init tracker`, структура папок, `.env.example`, `.gitignore`, `.gitattributes`, `Makefile` | `web/login.html`, `web/index.html` (каркас с навигацией) | `web/css/tokens.css` (все токены, светлая тема) |
| `docker-compose.yml` (postgres + api, healthcheck) | `web/js/api.js` (все вызовы к бэку, ApiError, перехват 401) | `web/css/base.css` (reset, типографика, layout страницы) |
| `db/schema.sql` (все таблицы + индексы, идемпотентный) | `web/js/state.js` (store, subscribe/set/get, structuredClone) | `web/css/components.css` (button, input, card, badge, skeleton) |
| `cmd/api/main.go` (wiring, embed schema, статика) | `web/js/ui.js` (esc, fmtDate, toast, debounce) | `web/css/pages.css` (шапка, колокольчик, пустые состояния) |
| `api/openapi.yaml` (все 23 endpoint, 18 schemas) | `web/js/router.js` (фильтры в URL, history.replaceState) | `web/sw.js` (push listener + notificationclick) |
| `cmd/genvapid/main.go` (генерация VAPID) | `web/js/task-modal.js` (форма создания/редактирования) | `web/js/notifications.js` (polling, markRead, markAllRead, бейдж) |
| `README.md` + docs | | `web/js/insights.js` (рендер подсказок из API) |
| | | `web/js/list.js` (таблица, рендер строки) |

**Интеграция дня 1 (к вечеру):** FE1 показывает `api.js` с `POST /api/auth/register` на мок-сервере. FE2 показывает layout с токенами и пустой `index.html`. Конечный артефакт: пустой `index.html` с шапкой, колокольчиком и боковой панелью.

**Definition of Done (середина дня 1 — scaffold и контракты):**
- [ ] `docker compose up -d postgres`, `go run ./cmd/api` стартует без ошибок
- [ ] `api/openapi.yaml` полный (все 23 operationId, 18 schemas)
- [ ] `api.js` содержит все вызовы
- [ ] `tokens.css` содержит все цвета, размеры, шрифты
- [ ] `index.html` загружается, `login.html` загружается
- [ ] Конечные контракты зафиксированы и отправлены в общий чат

| Backend | FE1 | FE2 |
|---|---|---|
| `store/users.go`, `service/auth.go` (register, login, bcrypt, JWT, cookie, middleware) | `web/js/kanban.js` (3 колонки, dragstart, dragover, drop) | `web/js/list.js` (сортировка по клику на заголовок) |
| `store/tasks.go`, `service/tasks.go` (create, list, patch, delete) | `web/js/task-modal.js` (привязка к kanban: открытие/закрытие) | `web/js/dashboard.js` (счётчики, вызов `/api/stats`) |
| `store/tasks.go` (фильтры, search, sort, pagination) | `web/js/kanban.js` (завершение DnD: call api.setStatus, optimistic update) | `web/css/` (стили модалки задачи) |
| `store/stats.go` (`GET /api/stats`) | `web/js/state.js` (загрузка/ошибка, sync с api.listTasks) | `web/js/notifications.js` (открытие модалки ленты, markRead) |
| `auth/middleware.go` (authRequired, extract UserID, cookie) | `web/js/app.js` (bootstrap: проверка сессии, связывание модулей) | `web/js/insights.js` (вызов insights, рендер карточек) |
| `store/notifications.go` (создание notification при due reminder) | | `web/css/pages.css` (пустые состояния, скелетоны) |

**Интеграция:** FE1 подключает kanban к реальному API. FE2 подключает дашборд (`GET /api/stats`) и фильтры к `GET /api/tasks`.

**Definition of Done (конец Д1):**
- [ ] Регистрация → вход → пустая доска
- [ ] Создание задачи через модалку → задача появляется в `todo`
- [ ] `GET /api/tasks` → список с фильтрами работает
- [ ] Kanban-колонки с карточками
- [ ] Дашборд показывает 0/0/0/0
- [ ] `go test ./...` зелёные (минимум: auth register success, 409 conflict, task create success, 400 bad input)
- [ ] FE1 и FE2 не видят чужие задачи

### День 2: напоминания и push

| Backend | FE1 | FE2 |
|---|---|---|
| `store/reminders.go` (create, delete, list) | `web/js/reminders.js` (UI напоминаний в модалке, создание/удаление) | `web/js/list.js` (фильтры: select статуса, select приоритета, input поиска) |
| `service/reminders.go` (валидация: fire_at > now, ≤5) | `web/js/kanban.js` (dragover подсветка колонки, визуал is-dragging) | `web/js/list.js` (сохранение фильтров в URL, readURL) |
| `scheduler/scheduler.go` (ticker, fetchDue, транзакция, grace window) | `web/js/app.js` (полная связка: state → render всех модулей) | `web/js/dashboard.js` (подсветка overdue/due_soon в дашборде) |
| `store/notifications.go` (list, markRead, markAllRead) | `web/js/reminders.js` (лимит 5, ошибка 422 под полем) | `web/js/notifications.js` (бейдж, модалка ленты) |
| `push/sender.go` (webpush-go, VAPID, обработка 404/410) | | |
| `store/tasks.go` (overdue, due_soon, reminder_count в DTO) | | |
| `insights/insights.go` (все 8 правил) | | |
| `internal/transport/httpapi/` (handler для insights, reminders, push) | | |

**Интеграция:** FE1 подключает reminders UI к `POST /api/tasks/{id}/reminders`. FE2 подключает фильтры к `GET /api/tasks` и дашборд к статистике.

**Definition of Done (конец Д2):**
- [ ] Создание напоминания → запись в БД → в списке задач `reminder_count > 0`
- [ ] Планировщик → notification в БД при `fire_at`
- [ ] In-app лента обновляется (колокольчик)
- [ ] Insights выдают подсказки при тестовых данных
- [ ] Push-подписка создаётся через API
- [ ] FE2: фильтры сохраняются в URL, перезагрузка → состояние восстановлено

### День 3: полировка + docker + полная интеграция

| Backend | FE1 | FE2 |
|---|---|---|
| Тесты: `task_handler_test.go` (CRUD + auth, ≥8 кейсов) | `web/js/kanban.js` (запасной путь: select статуса на карточке) | `tokens.css` → `[data-theme="dark"]` (тёмная тема) |
| `Dockerfile` (multi-stage, distroless) | `web/js/state.js` (ошибки API → toast, retry) | `web/css/pages.css` (empty states, skeleton анимации) |
| Grace window, cleanup dead push endpoints | `web/js/app.js` (полная связка всех модулей) | `web/css/components.css` (focus-visible, hover states) |
| `cmd/api/main.go`: embed статики, /sw.js | `web/js/task-modal.js` (визуальная обратная связь drag&drop) | `web/login.html` (дизайн, ошибки валидации) |
| `README.md` (быстрый старт) | `web/sw.js` (тест на localhost, совместно с FE1) | `web/login.html` (дизайн, ошибки валидации) |

**Интеграция:** Полный цикл — регистрация → создание задачи → напоминание → пуш (на localhost) → in-app лента → insights → удаление.

**Definition of Done (конец Д3):**
- [ ] `docker compose up -d --build` → приложение доступно на `localhost:8080`
- [ ] `go test ./...` зелёные (минимум 8 тест-кейсов)
- [ ] `gofmt -l .` пуст
- [ ] `go vet ./...` без замечаний
- [ ] Push на localhost работает (если Chrome/Edge)
- [ ] In-app лента работает при любом раскладе
- [ ] Тёмная тема переключается (если S-1 реализована)
- [ ] Запасной путь (select статуса) работает на карточке

### День 4: только баги, подготовка к хакатону

- [ ] Все открытые баги, критичные для демо (M-1 → M-16)
- [ ] Финальный прогон сценария `10-demo-script.md` (время ≤ 4 мин)
- [ ] `README.md` с инструкцией по запуску
- [ ] **Feature freeze:** никаких новых фич, даже «маленьких»

## 3. Порядок жертв (order of sacrifice)

Если к вечеру Д3 что-то не готово — режем по порядку, начиная с конца:

1. **Tёмная тема** (S-1) → откладывается на потом
2. **Web Push** → режется полностью, остаётся только in-app лента
3. **Drag & drop** → заменяется select статуса (карточка → dropdown → `api.setStatus`)
4. **Визуальная обратная связь DnD** (подсветка колонки, is-dragging) → заменяется кнопкой «Изменить» → select статуса
5. **Insights** → если совсем нет времени, показать блок «Аналитика» без подсказок (счётчики работают)

**Никогда не режем:** M-1 (регистрация), M-3 (изоляция), M-4 (создание задачи), M-5/M-6 (смена статуса), M-7 (канбан), M-9 (фильтры), M-11/M-12 (напоминания + in-app лента), M-16 (docker compose up).

## 4. Чек-лист готовности к хакатону (конец Д4)

- [ ] Репозиторий на GitHub (или GitLab) — клон доступен всем троим
- [ ] `.env.example` committed, `.env` в `.gitignore`
- [ ] `docker-compose.yml` в репозитории
- [ ] `go.mod` с `go 1.27`
- [ ] `docker compose up -d --build` работает на каждом ноутбуке
- [ ] Сценарий демо отработан ≥ 2 раза вслух
- [ ] Запасной ноутбук с проектом (если основной сломается)
- [ ] Батарейка заряжена, проектор проверен

## 5. Правила работы с AI-агентами

Все участники используют AI-агентов (включая меня) для генерации кода. Правила для AI — в `AGENTS.md`. Кратко:
- AI не меняет `api/openapi.yaml` без явного указания
- AI не добавляет зависимости без согласования
- AI не трогает файлы вне своей зоны ответственности (см. «Область владения» в `AGENTS.md`)
- После генерации кода — **обязательный** `go build ./... && go test ./...`
