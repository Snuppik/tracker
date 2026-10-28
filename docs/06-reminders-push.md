# 06. Напоминания и Web Push

Фича P-2: пользователь сам создаёт напоминание к задаче; уведомление доставляется на устройство пользователя, который создал напоминание (в модели P-1 это = владелец задачи).
Два уровня доставки: **in-app лента** (must-have, работает всегда) + **Web Push** (should-have, работает в secure context).

## 1. Напоминания в UI

Напоминания живут внутри детали задачи (открывается по клику на карточку или «Изменить»):

```
[Модалка задачи]
  ─── Название, Описание, Приоритет, Срок ───
  [Вкладка "Напоминания"]
    🔔 Уведомление 1: "через 1 час" — fire_at: 29 окт, 17:30 [x]
    🔔 Уведомление 2: "через 3 часа" — fire_at: 29 окт, 19:30 [x]
    [+] Новый таймер: [datetime-local] [текст ≤200] [Сохранить]
    Лимит: 2 / 5
```

**Правила валидации на фронте:**
- `fire_at` ≥ `now - 60s` (чтобы не «пропустить» и не создать в прошлом).
- `fire_at` ≤ `now + 5 лет`.
- «Сохранить» disabled, если `fire_at` не выбран.
- Кнопка «Сохранить» disabled, если `fire_at` < `now - 60s` (client-side защита от часовых сбоев).
- Серверная валидация — окончательная, ответ с `400`/`422` → показать ошибку под полем.

**UX-правила:**
- После создания — моментально добавляется в список (оптимистично), без ожидания сервера.
- `fire_at` показывается в относительном формате: «через 1 час», «через 2 дн.».
- Удаление — крестик рядом с каждым напоминанием, без диалога подтверждения (одно действие, не критичное).
- При закрытии модалки — сериализация: если пользователь создал 2 напоминания, закрыл, вернулся — они сохраняются. (Не «сбросить при закрытии».)

## 2. Планировщик (серверная часть)

Подробная архитектура — `02-architecture.md` §4. Здесь — специфика напоминаний.

### 2.1. Тикер и выборка

```go
// scheduler/scheduler.go — один вызов Run(ctx) из main.go
ticker := time.NewTicker(cfg.ReminderTickSeconds) // по умолчанию 30
defer ticker.Stop()

for {
    select {
    case <-ctx.Done(): return
    case <-ticker.C:
        batch := store.FetchDueReminders(ctx, time.Now().UTC(), 50)
        for _, r := range batch {
            scheduleReminderWork(ctx, r) // асинхронно, чтобы не блокировать тик
        }
    }
}
```

`FetchDueReminders`:
```sql
SELECT id, task_id, user_id, fire_at, message FROM reminders
WHERE status = 'pending' AND fire_at <= $1
ORDER BY fire_at
LIMIT 50 FOR UPDATE SKIP LOCKED;
```

### 2.2. Транзакция на каждое напоминание

```
BEGIN
  -- 1) задача всё ещё жива и не закрыта? иначе напоминание не доставляем
  SELECT t.status FROM tasks t WHERE t.id = $task_id FOR UPDATE;
  --   строки нет (задача удалена)          → reminder status='expired', COMMIT, выход
  --   status = 'done'                      → reminder status='expired', COMMIT, выход

  -- 2) вставляем уведомление в ленту (источник истины)
  INSERT INTO notifications (id, user_id, task_id, kind, title, body, created_at)
  VALUES ($uuid, $user_id, $task_id, 'reminder', $task_title, $message, now());

  -- 3) помечаем напоминание отправленным
  UPDATE reminders SET status='sent', sent_at=now(), attempts=0
  WHERE id = $reminder_id AND status='pending';
COMMIT
```

Внимание к порядку: шаг 1 читает состояние **в той же транзакции**, что и запись. Отдельная проверка «задача не done» до транзакции гонит: пользователь мог закрыть задачу между проверкой и вставкой уведомления, и мы отправили бы пуш по выполненной задаче.

Если `INSERT` падает (например, FK violation — задача удалена параллельно) — откат, напоминание осталось `pending` и «дождётся» следующего тика, где шаг 1 корректно пометит его `expired`. Если `INSERT` успешен, а `UPDATE` напоминания упал — **пуш может прийти дважды** (на следующем тике). Осознанный компромисс: лучше одно повторное уведомление, чем потерянное.

Отправка Web Push выполняется **после** коммита — тогда в ленте уже есть запись, на которую можно сослаться из `notificationclick`.

### 2.3. Grace window

Если сервер был выключен, напоминания «накопятся» при запуске. Правило:

```
if now() - fire_at > REMINDER_GRACE_MINUTES (по умолч. 10):
    status = 'expired'   // не спамим
else:
    статус 'sent'        // как обычно
```

Причина: напоминание «через 10 минут» в 19:00, сервер упал на 3 часа. В 22:01 запускается планировщик — если разослать 200 «просроченных на 3 часа» уведомлений, это сломает UX.

### 2.4. Отмена напоминаний

Правило `M-5` + `04-api.md`: при смене статуса задачи в `done` → все `pending`-напоминания → `expired` в той же транзакции. Реализуется в `service/changeStatus`:

```go
if newStatus == "done" {
  tx.Exec("UPDATE reminders SET status='expired' WHERE task_id=$1 AND status='pending'", taskID)
}
```

## 3. In-app уведомления (лента)

Объект `notification` создаётся планировщиком. Фронт опрашивает:

```
GET /api/notifications?unread=true&limit=50
```

**Polling-цикл (notifications.js):**
```js
const POLL_MS = 30_000;
let timer = setInterval(pollNotifications, POLL_MS);
document.addEventListener('visibilitychange', () => {
  if (document.hidden) clearInterval(timer);
  else { timer = setInterval(pollNotifications, POLL_MS); pollNotifications(); }
});
```

Почему polling, а не SSE/WebSocket: `02-architecture.md` §6 — 1 запрос / 30 с, ноль состояния на бэке, ноль проблем при перезапуске API. Задержка 30 с допустима; если пользователю критично быстрее — он не закроёт вкладку браузера, и polling продолжится.

**Бейдж в колокольчике:** `state.set({ unread: data.unread })` → `notifications.js` обновляет бейдж. Всегда берётся из ответа (не инкремент на фронте) — иначе счётчик рассинхронизируется при двух открытых вкладках.

**«Прочитать все»:** `POST /api/notifications/read-all` → `state.set({ notifications: updated, unread: 0 })`.

## 4. Web Push VAPID

### 4.1. Взаимодействие компонентов

```
┌──────────────┐
│  Frontend     │
│  sw.js        │ ← PushEventListener → showNotification()
│  notifications│ ← PushManager.subscribe() → PUT /api/push/subscription
└──────┬───────┘
       │  (1) GET /api/push/public-key
       │  (2) navigator.serviceWorker.register('/sw.js')
       │  (3) sw.addEventListener('push', ...)
       │  (4) PushManager.subscribe({ userVisibleOnly: true, applicationServerKey })
       │  (5) PUT /api/push/subscription { endpoint, keys }
       ▼
┌───────────────────┐
│  Backend           │
│  scheduler         │ ← INSERT notification + PUSH to endpoint(s)
│  push/sender.go    │ ← webpush-go + VAPID keys из env
└────────────────────┘
```

### 4.2. `sw.js` — Service Worker

```js
self.addEventListener('push', (event) => {
  const data = event.data?.json() ?? {};
  event.waitUntil(
    self.registration.showNotification(data.title ?? 'Tracker', {
      body: data.body ?? '',
      icon: '/img/icon-192.png',   // необязательно: если файла нет, браузер покажет значок сайта
      tag: data.task_id ?? 'default',
      data: { url: `/` },
    })
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil(clients.matchAll({type: 'window'}).then(wins => {
    for (const w of wins) {
      if (w.url.includes('/')) { w.focus(); return; }
    }
    return clients.openWindow('/');
  }));
});
```

**Важно:** `sw.js` обязан отдаваться с `Content-Type: text/javascript` и **без** `Cache-Control: immutable` (иначе Service Worker не обновится при `sw.js` изменении на сервере). В `router.go` Go:

```go
case "/sw.js":
    w.Header().Set("Content-Type", "text/javascript")
    w.Header().Set("Cache-Control", "no-cache")
    http.ServeFile(w, r, "web/sw.js")
```

### 4.3. Подписка

Порядок на фронте (когда пользователь первый раз заходит):

1. `const key = await api.vapidKey()` — получаем публичный VAPID-ключ.
2. `const sub = await navigator.serviceWorker.ready.then(sw => sw.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: urlBase64ToUint8Array(key) }))`
3. `const raw = JSON.parse(sub.toJSON())` — браузер возвращает `{endpoint, keys:{p256dh, auth}}`.
4. `await api.putPush(raw)` — сохраняем на сервере.

**Разрешение на уведомления** (`Notification.requestPermission()`) — **требуется жест пользователя** в Chrome 104+ (`00-decisions.md` §3). Поэтому:
- кнопка «Включить уведомления» в настройках/подвале;
- при клике: `Notification.requestPermission()` → по успеху → подписка.

### 4.4. Серверная отправка (push/sender.go)

Библиотека: `github.com/SherClockHolmes/webpush-go`.

```go
import webpush "github.com/SherClockHolmes/webpush-go"

// Options
opts := &webpush.Options{
    Subscriber:      cfg.VAPIDSubject,   // "mailto:..."
    VAPIDPublicKey:  cfg.VAPIDPublic,
    VAPIDPrivateKey: cfg.VAPIDPrivate,
    TTL:             30,                 // секунды хранения у провайдера
    AuthScheme:      webpush.WebPush,    // Chrome требует "webpush", не "vapid"
}

// Отправка
resp, err := webpush.SendNotification([]byte(`{"title":"Task","body":"Message","task_id":"id"}`), subscription, opts)
// resp.StatusCode:
//   200 → OK
//   404 → подписка не существует (endpoint устарел) → DELETE из БД
//   410 → подписка удалена навсегда (endpoint устарел) → DELETE из БД
```

`404/410` — чистим подписку (`DELETE FROM push_subscriptions WHERE endpoint=$1`). **Важно:** не блокировать планировщик на чистке — выполнять параллельно.

### 4.5. Формат полезной нагрузки пуша

```json
{ "title": "Сдать отчёт", "body": "За час до дедлайна", "task_id": "0198..." }
```

В браузере `event.data.json()` — тот же объект. `sw.js` показывает `title` + `body`, на `notificationclick` → фокус вкладки и (опционально) переход к задаче.

### 4.6. Генерация VAPID-ключей

```bash
go run ./cmd/genvapid
# Вывод:
# VAPID_PUBLIC_KEY = <base64url>
# VAPID_PRIVATE_KEY = <base64url>
# VAPID_SUBJECT = mailto:demo@tracker.local
```

Пользователь копирует в `.env`. Ключи генерируются один раз, живут в `.env` и в Docker-сборке — в файле `env.prod` (никогда в git).

## 5. Ограничения Web Push — прочитать перед демо

| Сценарий | Результат | Действие |
|---|---|---|
| `localhost:8080` / `127.0.0.1:8080` | ✅ Push работает | Демонстрация на localhost |
| `http://192.168.x.x:8080` (доступ по LAN) | ❌ `PushManager` отсутствует | Демонстрировать in-app-уведомления. Пуш не показывать |
| HTTPS-домен | ✅ | Любой продакшен |
| Safari iOS (не PWA) | ❌ | Нет поддержки |
| Safari iOS (PWA, добавлен на домашний экран, ≥16.4) | ✅ | Поддержка |
| Edge / IE | ❌ Edge — push работает (на Chromium), IE — нет | Нет поддержки IE |
| Firefox Android | ⚠️ Mozilla Push Service | Работает, но задержка может быть 2–5 мин |
| Firefox Desktop | ✅ | При `AuthScheme` = `webpush` (см. выше) |
| Chrome Android | ✅ | Быстро, надёжно |
| Chrome Desktop Linux (без notification-daemon) | ⚠️ | Уведомление не всплывает, `sw.js` `showNotification` вернёт ошибку. Решение: установить `libnotify-bin` и `dunst`/`mako` |

**Главный риск для хакатона:** жюри попросит показать с другого ноутбука по IP. **Ответ:** «Push-уведомление — демонстрация для localhost; in-app-лента работает из любой точки сети. Покажем, что уведомление пришло в колокольчике».

## 6. Обработка «мёртвых» эндпоинтов

Push-сервисы периодически удаляют неактивные подписки. На сервере:

```
if resp.StatusCode == 404 || resp.StatusCode == 410:
    DELETE FROM push_subscriptions WHERE endpoint = $1
```

Если пользователь отозвал разрешение в браузере — подписка «протухает» молча, без `pushsubscriptionchange` event (браузер не шлёт event при отзыве). Поэтому: раз в N дней (например, раз в 24 ч) — `GET /api/push/public-key` обновляет `last_seen_at`. Если `last_seen_at > 30 дней` — подписка помечается неактивной и не рассылается.

## 7. Чек-лист тестирования напоминаний + пуша

- [ ] Создать задачу → создать напоминание на `now + 60s` → проверить, что напоминание появилось в модалке
- [ ] Дождаться `fire_at` → проверить запись в in-app ленте (колокольчик, unread бейдж, текст)
- [ ] Проверить, что `due_at` < 24 ч → в блоке «Аналитика» есть подсказка `due_soon_cluster` (см. `07-ai-insights.md`)
- [ ] Открыть `localhost:8080` в Chrome → «Включить уведомления» → получить системное всплывающее окно
- [ ] `fire_at` наступил → системное уведомление пришло
- [ ] Клик по системному уведомлению → фокус вкладки, вкладка с `index.html`
- [ ] Закрыть вкладку → `fire_at` наступает → системное уведомление пришло (push работает даже при закрытой вкладке)
- [ ] Отменить подписку в настройках Chrome → `DELETE /api/push/subscription` → повторная подписка
- [ ] Создать 6-е напоминание к задаче → `422 limit_exceeded`
- [ ] Сменить статус задачи на `done` → pending-напоминания → `expired` (проверить в БД и ленте)
- [ ] Выключить API на 15 мин → создать напоминание на `now + 10s` → запустить API → напоминание → `expired` (grace window)
- [ ] Напоминание на `now + 5s` → API запущен → `sent` (не в grace window)

## 8. Troubleshooting

| Симптом | Причина | Решение |
|---|---|---|
| `PushManager` не определён | Не secure context | Проверить `location.protocol === 'https:' || location.hostname === 'localhost' || location.hostname === '127.0.0.1'` |
| Подписка создаётся, пуш не приходит | Неправильный AuthScheme | `webpush.WebPush` для Chrome, `webpush.Vapid` для Firefox |
| Подписка создаётся, пуш не приходит | VAPID-ключи не совпадают в env и `cmd/genvapid` | Перезапустить API с правильными ключами |
| Пуш не приходит на Firefox | AuthScheme `webpush` | Firefox требует `AuthScheme: webpush.Vapid` (не `WebPush`) |
| `sw.js` 404 | Go отдаёт его как статику, но не с `Content-Type: text/javascript` | Проверить `router.go` — явный case для `/sw.js` |
| `sw.js` не обновляется | Браузер закэшировал старый SW | `Cache-Control: no-cache` на `/sw.js`, `navigator.serviceWorker.register(..., {updateViaCache: 'imports'})` |
| Уведомление пришло, но без title | `sw.js` читает `event.data.text()`, а не `.json()` | `event.data.json()` |
| `Notification.requestPermission` не показывает диалог | Chrome требует user gesture | Вызывать только по клику кнопки «Включить уведомления» |
| Всплывает «Ошибка показа уведомления» | Нет notification-daemon на Linux | `apt install libnotify-bin && dunst &` |
