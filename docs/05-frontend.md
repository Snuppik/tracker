# 05. Фронтенд

Vanilla HTML + CSS + ES-модули. Сборщика нет, фреймворка нет, Node не ставим (`00-decisions.md` T-12/T-13).
Статику отдаёт Go-процесс из папки `web/` (T-11) — один origin, поэтому `fetch` идёт с `credentials: 'same-origin'` и без CORS.

## 1. Структура и ответственность файлов

```
web/
├── login.html            // только формы входа и регистрации
├── index.html            // приложение: шапка, панель аналитики, доска/список, модалки
├── sw.js                 // Service Worker: push + показ уведомлений
├── css/
│   ├── tokens.css        // ВСЕ значения дизайна. Единственный источник (контракт для FE1)
│   ├── base.css          // reset, типографика, layout страницы, grid/flex утилиты
│   ├── components.css    // button, input, card, badge, modal, toast, tooltip, skeleton
│   └── pages.css         // специфика доска/список/аналитика/логин
└── js/
    ├── api.js            // ВСЕ запросы к серверу. Наружу — только типизированные функции
    ├── state.js          // единое состояние + подписки (pub/sub)
    ├── ui.js             // esc(), fmtDate(), fmtRelative(), toast(), modal(), debounce()
    ├── router.js         // состояние фильтров/вида в URL (history.replaceState)
    ├── task-modal.js     // форма создания/редактирования задачи + вкладка напоминаний
    ├── reminders.js      // список напоминаний задачи, добавление/удаление
    ├── kanban.js         // 3 колонки + drag&drop
    ├── list.js           // таблица + сортировка
    ├── dashboard.js      // счётчики
    ├── insights.js       // блок «Аналитика»
    ├── notifications.js  // колокольчик, лента, polling, подписка на push
    └── app.js            // bootstrap: проверка сессии, связывание модулей, обработчики шапки
```

Правила:
1. **Ни одного `fetch()` вне `api.js`.** Ни одного прямого обращения к `document.cookie`.
2. **Ни одного цвета/размера/шрифта вне `tokens.css`.** В `components.css`/`pages.css` — только `var(--…)`.
3. Модули общаются через `state.js`, а не друг с другом напрямую. Исключение: `app.js` может импортировать всё.
4. Каждый модуль экспортирует `init(container)` и, где нужно, `render()`. Никаких «инициализаций на импорте» с побочными эффектами в глобальной области.

## 2. `api.js` — контракт

```js
const BASE = '';                      // тот же origin
async function request(path, { method = 'GET', body, query } = {}) {
  const url = new URL(path, location.origin);
  if (query) for (const [k, v] of Object.entries(query))
    if (v !== null && v !== undefined && v !== '') url.searchParams.set(k, v);

  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401 && !path.startsWith('/api/auth/')) {
    location.href = '/login';          // единая реакция на протухшую сессию
    throw new ApiError('unauthorized', 'Сессия истекла', []);
  }
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(data.error?.code ?? 'internal',
                                  data.error?.message ?? 'Ошибка сервера',
                                  data.error?.details ?? []);
  return data;
}

export const api = {
  register(input) { return request('/api/auth/register', { method: 'POST', body: input }); },
  login(input)    { return request('/api/auth/login',    { method: 'POST', body: input }); },
  logout()        { return request('/api/auth/logout',   { method: 'POST' }); },
  me()            { return request('/api/auth/me'); },

  listTasks(f)    { return request('/api/tasks', { query: f }); },
  getTask(id)     { return request(`/api/tasks/${id}`); },
  createTask(i)   { return request('/api/tasks', { method: 'POST', body: i }); },
  patchTask(id,p) { return request(`/api/tasks/${id}`, { method: 'PATCH', body: p }); },
  setStatus(id,s) { return request(`/api/tasks/${id}/status`, { method: 'PUT', body: { status: s } }); },
  deleteTask(id)  { return request(`/api/tasks/${id}`, { method: 'DELETE' }); },

  createReminder(taskId, r) { return request(`/api/tasks/${taskId}/reminders`, { method: 'POST', body: r }); },
  deleteReminder(id)        { return request(`/api/reminders/${id}`, { method: 'DELETE' }); },

  stats()      { return request('/api/stats'); },
  insights()   { return request('/api/insights'); },
  notifications(unread = false) { return request('/api/notifications', { query: { unread } }); },
  markRead(id) { return request(`/api/notifications/${id}/read`, { method: 'POST' }); },
  markAllRead(){ return request('/api/notifications/read-all', { method: 'POST' }); },

  vapidKey()   { return request('/api/push/public-key'); },
  putPush(sub)    { return request('/api/push/subscription', { method: 'PUT', body: sub }); },
  delPush(endpoint){ return request('/api/push/subscription', { method: 'DELETE', body: { endpoint } }); },
};

export class ApiError extends Error {
  constructor(code, message, details) { super(message); this.code = code; this.details = details; }
  /** Сообщение для конкретного поля формы */
  fieldError(field) { return this.details.find(d => d.field === field)?.message; }
}
```

`ApiError.fieldError` — причина, почему валидация живёт на сервере, а UI показывает её без дублирования правил.

## 3. `state.js` — минимум вместо фреймворка

```js
const state = {
  user: null,
  view: 'kanban',                  // 'kanban' | 'list'
  filters: { status: '', priority: '', search: '', sort: 'due_at', dir: 'asc' },
  tasks: [], total: 0,
  stats: null, insights: [],
  notifications: [], unread: 0,
  openTaskId: null,
  loading: { tasks: false, insights: false },
  error: null,
};

const listeners = new Set();
export const get = (sel) => sel ? structuredClone(select(state, sel)) : state;
export function set(patch) { Object.assign(state, patch); listeners.forEach(fn => fn(state)); }
export function subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); }
```

Почему так, а не «каждый модуль хранит своё»: задача, изменённая на доске, должна мгновенно обновить список, счётчики и колокольчик. Один стор = одна перерисовка.

**Перерисовка:** подписчики вызывают `render()` своего блока. Порядок в `app.js`: `set()` → подписчики обновляют DOM. Никакого виртуального DOM: при ≤200 задачах `innerHTML`-рендер блока занимает единицы миллисекунд.

## 4. Безопасность рендеринга (обязательно, это XSS)

Данные приходят из БД другого пользователя? Нет. **Но пользователь сам вводит текст в название задачи**, и он попадает в DOM. Правило:

```js
export const esc = (s) => String(s ?? '').replace(/[&<>"']/g,
  c => ({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[c]));
```

1. При использовании шаблонных строк **любое** пользовательское значение оборачивать в `esc()`.
2. Предпочтительный способ для интерактивных элементов — `document.createElement` + `textContent`.
3. `innerHTML` без `esc()` — нарушение, блокирующее приёмку. Особый контроль в `kanban.js`, `list.js`, `notifications.js`, `insights.js` (там подставляются `title`, `message`, `body`).
4. Никаких `javascript:`-ссылок и никаких `href` из пользовательских данных.

## 5. Kanban (`kanban.js`)

Разметка колонки:

```html
<section class="column" data-status="todo" aria-labelledby="col-todo-title">
  <header class="column-head">
    <h2 id="col-todo-title">К работе</h2><span class="count">7</span>
    <button class="btn-icon" data-action="add" aria-label="Добавить задачу">+</button>
  </header>
  <div class="column-body" data-drop-target>
    <!-- карточки -->
  </div>
</section>
```

Карточка:

```html
<article class="card" draggable="true" data-id="0198…" tabindex="0">
  <p class="card-title">Сдать отчёт</p>
  <div class="card-meta">
    <span class="badge badge-high">Высокий</span>
    <time class="card-due is-overdue" datetime="2026-10-29T15:30:00Z">просрочено на 2 ч</time>
    <span class="card-reminders" title="Напоминаний: 2">🔔 2</span>
  </div>
</article>
```

**Drag & drop (нативный HTML5):**

```js
// на карточке
card.addEventListener('dragstart', (e) => {
  e.dataTransfer.setData('text/plain', card.dataset.id);  // Firefox без этого НЕ начинает drag
  e.dataTransfer.effectAllowed = 'move';
  card.classList.add('is-dragging');
});
card.addEventListener('dragend', () => card.classList.remove('is-dragging'));

// на .column-body
body.addEventListener('dragover', (e) => {
  e.preventDefault();                       // без этого drop не сработает никогда
  e.dataTransfer.dropEffect = 'move';
  body.classList.add('is-over');
});
body.addEventListener('dragleave', () => body.classList.remove('is-over'));
body.addEventListener('drop', async (e) => {
  e.preventDefault();
  body.classList.remove('is-over');
  const id = e.dataTransfer.getData('text/plain');
  const status = body.closest('.column').dataset.status;
  await changeStatus(id, status);
});
```

`changeStatus` = оптимистичное обновление (S-4): `set({tasks: localUpdate})` → `api.setStatus` → при ошибке откат + toast. Это спасает демо от «лагов по 300 мс на каждое перетаскивание».

**Запасной путь (обязателен, не опционален):** на карточке в модалке и в списке — `<select>` со статусом, `change` → тот же `changeStatus`. Причин две: HTML5 DnD не работает на тач-устройствах и может вести себя нестабильно на конкретном ноутбуке для демо; на сцене должен быть способ показать смену статуса за 5 секунд без мыши.

**Клавиатура:** `Enter`/`Space` на карточке (у неё `tabindex="0"`) открывает модалку задачи.

## 6. Список (`list.js`)

Таблица: название · приоритет · срок · статус · напоминания · действия (Изменить/Удалить).
Клик по заголовку колонки «Срок»/«Приоритет»/«Создана» → меняет `sort`/`dir` → `router.write()` → `refresh()`. Активная сортировка помечена стрелкой и `aria-sort`.
Фильтры (`select` статуса, `select` приоритета, `input` поиска) живут в шапке и **синхронизируются с URL** (`?view=list&status=todo&priority=high&q=отчёт`) — это позволяет показать «а вот мой фильтр» повторной загрузкой и не теряет состояние при F5 (критерий M-6/M-9 на демо).

## 7. Дизайн-система (`tokens.css`)

```css
:root {
  /* Палитра. Семантические имена, не сырьё */
  --c-bg: #f6f7f9;        --c-surface: #ffffff;   --c-surface-2: #f0f1f4;
  --c-border: #dfe1e6;    --c-border-strong: #c1c7d0;
  --c-text: #17181c;      --c-text-2: #5e6c84;    --c-text-3: #9199a9;
  --c-accent: #2f6fed;    --c-accent-hover: #1f5bd0; --c-accent-soft: #eaf1ff;
  --c-danger: #d92d20;    --c-danger-soft: #fee4e2;
  --c-warn: #b54708;      --c-warn-soft: #fef0c7;
  --c-ok: #067647;        --c-ok-soft: #d1fadf;
  --c-prio-low: #5e6c84;  --c-prio-medium: #b54708; --c-prio-high: #d92d20;

  /* Типографика. Системный стек: никаких внешних шрифтов — демо может быть без сети */
  --font: system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans", sans-serif;
  --fs-12: .75rem; --fs-14: .875rem; --fs-16: 1rem; --fs-20: 1.25rem; --fs-24: 1.5rem; --fs-32: 2rem;

  /* Отступы: шкала 4px */
  --s-1: 4px; --s-2: 8px; --s-3: 12px; --s-4: 16px; --s-5: 24px; --s-6: 32px; --s-7: 48px;

  --r-sm: 4px; --r-md: 8px; --r-lg: 12px;
  --sh-1: 0 1px 2px rgba(16,24,40,.06), 0 1px 3px rgba(16,24,40,.10);
  --sh-2: 0 4px 8px -2px rgba(16,24,40,.10), 0 2px 4px -2px rgba(16,24,40,.06);
  --dur: 150ms;
}
[data-theme="dark"] { /* FE2: переопределяются ТОЛЬКО цвета, остальные токены те же */ }
```

Правила стиля: `rem` для текста, `var(--s-*)` для отступов, BEM-подобные имена (`.card-title`, `.btn-icon`, без вложенности глубже 2 селекторов), mobile-first media-queries (`@media (min-width: 768px)`, `1200px`), `:focus-visible` обязателен на всех интерактивах.

Соответствие статусов и приоритетов в UI (единое для доске, списка и бейджей):

| Значение | Подпись | Цвет |
|---|---|---|
| `todo` | К работе | нейтральный `--c-surface-2` |
| `in_progress` | В работе | `--c-accent-soft` / `--c-accent` |
| `done` | Готово | `--c-ok-soft` / `--c-ok` |
| `low` | Низкий | `--c-prio-low` |
| `medium` | Средний | `--c-prio-medium` |
| `high` | Высокий | `--c-prio-high` |
| просрочено | «просрочено на N ч» | `--c-danger` |
| ≤24 ч | «через N ч» | `--c-warn` |

## 8. Форматирование дат и времени

```js
export const fmtDue = (iso) => {…}   // «сегодня, 18:30», «завтра, 12:00», «29 окт, 18:30»
export const fmtRelative = (iso) => {…} // «через 2 ч», «просрочено на 15 мин»
```

`Intl.DateTimeFormat('ru-RU', {hour:'2-digit', day:'numeric', month:'short'})` — нативно, без библиотек. «Сегодня/завтра» — своя ветка на сравнении с локальной датой.
Важно: `due_at` приходит в UTC, браузер сам переводит в локальную зону при `new Date(iso)`. Никогда не парсить строку руками.

`datetime-local` возвращает **локальное время без зоны** → на сервер отдаём `new Date(value).toISOString()` (получится корректный UTC). Обратное направление (для отображения существующего напоминания в поле) — `fmtLocalInputValue(iso)`. Это самое частое место ошибок, на него есть тест-кейс в `08-roles-plan.md`.

## 9. Загрузка, ошибки, пустые состояния

| Состояние | Что показывает UI |
|---|---|
| Загрузка списка | 6 skeleton-карточек (`.skeleton` с анимацией `shimmer`), не «пустая доска» |
| Ошибка сети | Плашка «Не удалось загрузить задачи» + кнопка «Повторить» (овсянка из `state.error`) |
| Пустой список | Иллюстрация-заглушка (CSS, не картинка) + «Добавьте первую задачу» + кнопка |
| Ошибка валидации формы | Сообщение под конкретным полем из `ApiError.fieldError(name)` + `aria-invalid="true"` |
| 401 | Автоматический редирект на `/login` (уже в `api.js`) |

Toast-уведомления: `ui.toast(message, type)` — 3 с, `role="status"`, стек справа сверху, без `alert()`.

## 10. Модальные окна

Нативный `<dialog>` + `showModal()` — фокус-ловушка, `Esc`, затемнение фона и доступность бесплатно, 0 строк библиотечного кода.
Два диалога: «Задача» (создание/редактирование + вкладка напоминаний) и «Подтверждение удаления».
Удаление — только через диалог подтверждения (данные пользователя, отмена не предусмотрена).

## 11. Service Worker и push на фронте

Полный дизайн — `06-reminders-push.md`. Здесь — ответственность файлов:
`sw.js` (регистрация, `push` → `showNotification`, `notificationclick` → фокус вкладки), `notifications.js` (запрос разрешения по клику, подписка, отправка на сервер, polling ленты, бейдж).

## 12. Производительность и объёмы (бюджет, не «постарайтесь»)

- 0 внешних запросов (ни шрифтов, ни CDN) → приложение работает в офлайне. Это требование, т.к. на хакатоне Wi-Fi может не быть.
- Итог CSS ≤ 60 КБ, JS ≤ 40 КБ (без minification — нечем).
- Рендер доски ≤ 16 мс при 100 задачах: группировка по статусу выполняется один раз за `render()`, не в цикле.
- Опрос уведомлений: 1 запрос / 30 с, при `document.hidden` — пауза.

## 13. Что принимает FE2 у FE1 и наоборот

- FE1 не правит `tokens.css` — просит через чат; FE2 не правит `kanban.js`.
- FE2 передаёт FE1 готовую разметку карточки/колонки/строки списка с классами из `components.css`, FE1 навешивает поведение.
- Оба не трогают `index.html` одновременно: структура страницы — за FE2, слоты `<div id="board">`, `<div id="list">`, `<div id="insights">` — согласованы и зафиксированы в этом документе.
