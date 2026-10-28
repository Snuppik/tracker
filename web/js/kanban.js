/**
 * Kanban: 3 колонки и нативный HTML5 drag & drop.
 * Смена статуса выполняется оптимистично через обработчик app.js.
 */
import { get } from './state.js';
import { esc, fmtDue, fmtRelative, priorityLabel } from './ui.js';

const COLUMNS = [
  { status: 'todo', title: 'К работе' },
  { status: 'in_progress', title: 'В работе' },
  { status: 'done', title: 'Готово' },
];

let root = null;
let handlers = {};

function cardHtml(task) {
  let due;
  if (task.due_at) {
    const tone = task.overdue ? ' is-overdue' : task.due_soon ? ' is-soon' : '';
    const text = task.overdue ? fmtRelative(task.due_at) : fmtDue(task.due_at);
    due = `<time class="card-due${tone}" datetime="${esc(task.due_at)}" title="${esc(fmtDue(task.due_at))}">${esc(text)}</time>`;
  } else {
    due = '<span class="card-due">Без срока</span>';
  }

  const reminders = task.reminder_count > 0
    ? `<span class="card-reminders" title="Напоминаний: ${task.reminder_count}">🔔 ${task.reminder_count}</span>`
    : '';

  return `
    <article class="card" draggable="true" tabindex="0" data-id="${esc(task.id)}">
      <p class="card-title">${esc(task.title)}</p>
      <div class="card-meta">
        <span class="badge badge-${task.priority}">${esc(priorityLabel(task.priority))}</span>
        ${due}
        ${reminders}
      </div>
    </article>
  `;
}

function columnHtml(column, tasks) {
  return `
    <section class="column" data-status="${column.status}" aria-labelledby="col-${column.status}-title">
      <header class="column-head">
        <h2 id="col-${column.status}-title">${column.title}</h2>
        <span class="count">${tasks.length}</span>
        <button class="btn-icon" type="button" data-action="add" aria-label="Добавить задачу: ${column.title}">+</button>
      </header>
      <div class="column-body" data-drop-target>
        ${tasks.length ? tasks.map(cardHtml).join('') : '<p class="column-empty">Нет задач</p>'}
      </div>
    </section>
  `;
}

function renderSkeletons() {
  root.innerHTML = COLUMNS.map(() => `
    <section class="column">
      <div class="skeleton skeleton-head"></div>
      <div class="column-body">
        <div class="skeleton"></div>
        <div class="skeleton"></div>
      </div>
    </section>
  `).join('');
}

function renderError(message) {
  root.innerHTML = `
    <div class="banner-error">
      <span>${esc(message || 'Не удалось загрузить задачи')}</span>
      <button class="btn" type="button" data-action="retry">Повторить</button>
    </div>
  `;
}

function renderEmpty(hasFilters) {
  root.innerHTML = `
    <div class="empty">
      <div class="empty-art" aria-hidden="true">${hasFilters ? '⌕' : '✓'}</div>
      <h2>${hasFilters ? 'Ничего не найдено' : 'Добавьте первую задачу'}</h2>
      <p>${hasFilters
        ? 'Попробуйте изменить фильтры или поисковый запрос.'
        : 'Начните с одной задачи: название, приоритет и срок.'}</p>
      <button class="btn btn-primary" type="button" data-action="${hasFilters ? 'reset' : 'add'}">
        ${hasFilters ? 'Сбросить фильтры' : 'Создать задачу'}
      </button>
    </div>
  `;
}

function onClick(event) {
  const card = event.target.closest('.card');
  if (card) {
    handlers.onOpen?.(card.dataset.id);
    return;
  }

  const action = event.target.closest('[data-action]');
  if (!action) return;

  if (action.dataset.action === 'retry') handlers.onRetry?.();
  else if (action.dataset.action === 'reset') handlers.onResetFilters?.();
  else if (action.dataset.action === 'add') {
    const column = action.closest('.column');
    handlers.onAdd?.(column?.dataset.status ?? 'todo');
  }
}

function onKeydown(event) {
  const card = event.target.closest('.card');
  if (card && (event.key === 'Enter' || event.key === ' ')) {
    event.preventDefault();
    handlers.onOpen?.(card.dataset.id);
  }
}

function onDragStart(event) {
  const card = event.target.closest('.card');
  if (!card) return;

  event.dataTransfer.setData('text/plain', card.dataset.id);
  event.dataTransfer.effectAllowed = 'move';
  card.classList.add('is-dragging');
}

function onDragEnd(event) {
  event.target.closest('.card')?.classList.remove('is-dragging');
}

function onDragOver(event) {
  const body = event.target.closest('.column-body');
  if (!body) return;

  event.preventDefault();
  event.dataTransfer.dropEffect = 'move';
  body.classList.add('is-over');
}

function onDragLeave(event) {
  const body = event.target.closest('.column-body');
  if (body && !body.contains(event.relatedTarget)) body.classList.remove('is-over');
}

function onDrop(event) {
  const body = event.target.closest('.column-body');
  if (!body) return;

  event.preventDefault();
  body.classList.remove('is-over');

  const id = event.dataTransfer.getData('text/plain');
  const status = body.closest('.column')?.dataset.status;
  if (id && status) handlers.onStatusChange?.(id, status);
}

export function init(container, options = {}) {
  root = container;
  handlers = options;

  root.addEventListener('click', onClick);
  root.addEventListener('keydown', onKeydown);
  root.addEventListener('dragstart', onDragStart);
  root.addEventListener('dragend', onDragEnd);
  root.addEventListener('dragover', onDragOver);
  root.addEventListener('dragleave', onDragLeave);
  root.addEventListener('drop', onDrop);
}

export function render() {
  if (!root) return;

  const tasks = get('tasks');
  const filters = get('filters');
  const loading = get('loading');
  const error = get('error');

  if (loading.tasks && !tasks.length) {
    renderSkeletons();
    return;
  }

  if (error) {
    renderError(error);
    return;
  }

  const hasFilters = Boolean(filters.status || filters.priority || filters.search);
  if (!tasks.length) {
    renderEmpty(hasFilters);
    return;
  }

  const grouped = { todo: [], in_progress: [], done: [] };
  for (const task of tasks) grouped[task.status]?.push(task);

  root.innerHTML = COLUMNS.map((column) => columnHtml(column, grouped[column.status])).join('');
}
