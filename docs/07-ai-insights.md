# 07. Rule-based AI-подсказки

Детерминированный серверный движок. Без внешних LLM, без API-ключей, без «чёрного ящика». Результат — массив JSON-объектов, фронт просто рендерит.

**Почему rule-based вместо LLM:**
- На 4 дня — нет стабильной сети (хакатон), нет ключа, нет гарантии, что LLM-эндпоинт ответит до дедлайна.
- Детерминированность — плюс на защите: жюри спрашивает «почему такая подсказка?», ответ — «потому что 3 задачи с дедлайном сегодня».
- Результат не меняется «от настроения модели» — одинаковый запрос → одинаковый ответ.
- Если после MVP останется время → добавить LLM-кнопку «ИИ-анализ» как надстройку (см. `00-decisions.md` P-8).

## 1. API-контракт

```
GET /api/insights
```

Ответ:

```json
{
  "generated_at": "2026-10-26T19:20:00Z",
  "items": [
    {
      "code": "overdue_exists",
      "severity": "warning",
      "title": "2 задачи просрочены",
      "message": "Вам нужно вернуть эти задачи в работу или перенести дедлайн.",
      "task_ids": ["0198a3f1-...", "0198a40b-..."]
    }
  ]
}
```

Свойства объекта:

| Поле | Тип | Описание |
|---|---|---|
| `code` | enum | Код правила (используется для стабильного UI-рендера) |
| `severity` | enum | `info` (информативно), `warning` (рекомендуется действие), `critical` (критично) |
| `title` | string | Короткий заголовок (до 60 символов) |
| `message` | string | Пояснение (до 200 символов) |
| `task_ids` | uuid[] | Задачи, на которые ссылается подсказка (≤10) |

## 2. Список правил

### R-1: `no_tasks` (info)

**Когда:** у пользователя 0 задач.

**Зачем:** онбординг. Если пользователь зарегистрировался и видит пустую доску — подсказка «Начните с создания задачи».

**SQL:** `SELECT count(*) FROM tasks WHERE user_id = $1`. Если `= 0` → включить.

**Важно:** показывается **только один раз** за сессию (или только если нет задач вообще, а не только в текущем фильтре). Если пользователь удалил все задачи — подсказка снова актуальна.

### R-2: `overdue_exists` (warning)

**Когда:** ≥ 1 задача `status != 'done' AND due_at IS NOT NULL AND due_at < now()`.

**SQL:**
```sql
SELECT id, title FROM tasks
WHERE user_id = $1 AND status <> 'done' AND due_at IS NOT NULL AND due_at < now()
ORDER BY due_at DESC LIMIT 10;
```

**Title:** `{count} задач просрочено`
**Message:** «Вам нужно вернуть эти задачи в работу или перенести дедлайн.»

### R-3: `due_soon_cluster` (warning)

**Когда:** ≥ 3 задачи `status != 'done' AND due_at IS NOT NULL AND due_at >= now() AND due_at < now() + 24h`.

**SQL:**
```sql
SELECT id, title, due_at FROM tasks
WHERE user_id = $1 AND status <> 'done' AND due_at IS NOT NULL
  AND due_at >= now() AND due_at < now() + interval '24 hours'
ORDER BY due_at LIMIT 10;
```

**Title:** `{count} задач горят в ближайшие 24 часа`
**Message:** «Суммарно на сегодня приходится {count} дедлайна — вероятно, план нереалистичен. Начните с «{первая задача}».»

### R-4: `overloaded_day` (warning)

**Когда:** в любой день (дату дедлайна) ≥ 6 задач с `due_at` (не обязательно просроченных).

**SQL:**
```sql
SELECT date(due_at AT TIME ZONE 'UTC') AS day, count(*),
       array_agg(id ORDER BY due_at) AS ids
FROM tasks
WHERE user_id = $1 AND status <> 'done' AND due_at IS NOT NULL
GROUP BY 1 HAVING count(*) >= 6
ORDER BY day LIMIT 1;
```

**Title:** «Перегруз на {day} — {count} задач»
**Message:** «На {day} запланировано {count} задач. Рассмотрите перенос части задач на другие дни.»

**День** форматируется как `29 окт 2026`.

### R-5: `no_deadline` (info)

**Когда:** ≥ 2 задачи `status = 'in_progress' AND due_at IS NULL`.

**SQL:**
```sql
SELECT id, title FROM tasks
WHERE user_id = $1 AND status = 'in_progress' AND due_at IS NULL
ORDER BY created_at DESC LIMIT 10;
```

**Title:** «Задачи без срока: {count}»
**Message:** «Задачи в работе без дедлайна могут висеть бесконечно. Установите срок для {первая задача} и {вторая задача}.»

### R-6: `stale_in_progress` (info)

**Когда:** ≥ 1 задача `status = 'in_progress' AND updated_at < now() - 3*interval '1 day'`.

**SQL:**
```sql
SELECT id, title, updated_at FROM tasks
WHERE user_id = $1 AND status = 'in_progress'
  AND updated_at < now() - interval '3 days'
ORDER BY updated_at LIMIT 5;
```

**Title:** «Задачи без изменений 3+ дней: {count}»
**Message:** «{первая задача} не менялась давно. Перенесите в «К работе» или завершите, если она уже не актуальна.»

### R-7: `high_priority_in_todo` (info)

**Когда:** ≥ 1 задача `status = 'todo' AND priority = 'high' AND due_at IS NOT NULL AND due_at <= now() + 48h`.

**SQL:**
```sql
SELECT id, title, due_at FROM tasks
WHERE user_id = $1 AND status = 'todo' AND priority = 'high'
  AND due_at IS NOT NULL AND due_at <= now() + interval '48 hours'
ORDER BY due_at LIMIT 5;
```

**Title:** «Высокий приоритет, но ещё не начато: {count}»
**Message:** «{первая задача} имеет высокий приоритет и дедлайн {due_at}. Рассмотрите начало работы.»

### R-8: `weekly_progress` (info)

**Когда:** ≥ 1 задача завершена за последние 7 дней.

**SQL:**
```sql
SELECT count(*) FROM tasks
WHERE user_id = $1 AND status = 'done' AND updated_at >= now() - interval '7 days';
```

**Title:** «За неделю завершено: {count} задач»
**Message:** «Хорошая динамика! Продолжайте в том же духе.»

**Показывать только если `count >= 1`**, и **не показывать если count <= 1** (один завершённый — недостаточно для мотивации).

## 3. Порядок и лимит

- Правила выполняются в порядке `R-1 → R-8`.
- **Максимум 5 подсказок** в ответе (отсечение по индексу массива).
- Если результат пуст — `{ "generated_at": "...", "items": [] }` — `200`, а не `204` (фронт проверяет `items.length === 0`, а не `null`).
- `generated_at` = `time.Now().UTC().Format(time.RFC3339)` — для отладки и отсечения кэша.

## 4. Реализация (insights/insights.go)

Структура правила:

```go
type rule struct {
    code        string
    titleTmpl   string   // Go-шаблон с .Count, .FirstTask, .Day
    msgTmpl     string
    severity    string
    fetch       func(ctx context.Context, db *sql.DB, userID uuid.UUID) (*ruleResult, error)
}

type ruleResult struct {
    TaskIDs []uuid.UUID `json:"task_ids"`
    FirstTask string    `json:"first_task"`
    SecondTask string   `json:"second_task"`
    Count     int       `json:"count"`
    Day       string    `json:"day"`
    DueAt     string    `json:"due_at"`
}
```

`fetch` — замыкает SQL-запрос и возвращает `ruleResult`. `fmt.Sprintf` на title/msgTmpl с полями результата. Один DB-вызов на правило.

## 5. Тесты

Каждое правило — 1 тест в `insights/insights_test.go`:
- **Given:** в тестовую БД вставлены {N} задач с заданными статусами/дедлайнами.
- **When:** вызван `Collect(ctx, db, userID)`.
- **Then:** `items` содержит подсказку с правильным `code`, `severity`, `task_ids`.

Пример: `TestRuleOverdue` — вставить 2 задачи `todo`, `due_at: now - 1h`. Ожидается `overdue_exists` с `count=2`.

## 6. Что НЕ входит

- Приоритизация задач (список «что сделать первым» — это `R-7 + R-3`, а не отдельный алгоритм).
- Декомпозиция задач (разделить «Сдать отчёт» на подзадачи — требует NLP, не rule-based).
- Оценка трудозатрат (у нас нет поля «оценка» — не добавлять ради AI).
- Прогнозирование (ML-модель — не за 4 дня).
- LLM-кнопка «Проанализировать» — если появится, это отдельный эндпоинт, не замена rule-based.

## 7. Связь с дашбордом и UI

`GET /api/insights` вызывается один раз при загрузке `index.html`. Результаты — блок в панели аналитики над доской. Каждая подсказка — карточка с иконкой по `severity` (info = ℹ️, warning = ⚠️, critical = 🚨), заголовком, текстом и кнопкой «Показать задачи» (фильтрует список по `task_ids`).

Кнопка «Показать задачи» = `router.setFilters({ status: '', priority: '', search: '' })` + `set({ view: 'list' })` + `state.tasks = getTasksByID(items.task_ids)`. Если задач много (>10) — показать «Показать все» с ограничением в 10.
