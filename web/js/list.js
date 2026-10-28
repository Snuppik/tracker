/**
 * Список задач: таблица, сортировка по заголовкам и запасной select статуса.
 */
import { get } from './state.js';
import {
  esc,
  fmtDateTime,
  fmtDue,
  fmtRelative,
  priorityLabel,
  statusLabel,
} from './ui.js';

let root = null;
let handlers = {};

const statusOptions = (current) => ['todo', 'in_progress', 'done'].map((value) => (
  `<option value="${value}"${value === current ? ' selected' : ''}>${statusLabel(value)}</option>`
)).join('');

function dueHtml(task) {
  if (!task.due_at) return 'Без срока';

  const tone = task.overdue ? ' is-overdue' : task.due_soon ? ' is-soon' : '';
  const text = task.overdue ? fmtRelative(task.due_at) : fmtDue(task.due_at);
  return `<time class="card-due${tone}" datetime="${esc(task.due_at)}" title="${esc(fmtDue(task.due_at))}">${esc(text)}</time>`;
}

function rowHtml(task) {
  return `
    <tr>
      <td><button class="task-name" type="button" data-open="${esc(task.id)}">${esc(task.title)}</button></td>
      <td><span class="badge badge-${task.priority}">${esc(priorityLabel(task.priority))}</span></td>
      <td>${dueHtml(task)}</td>
      <td>
        <select class="status-select" data-status-id="${esc(task.id)}" aria-label="Статус задачи «${esc(task.title)}»">
          ${statusOptions(task.status)}
        </select>
      </td>
      <td>${esc(fmtDateTime(task.created_at))}</td>
      <td>${task.reminder_count > 0 ? `🔔 ${task.reminder_count}` : '—'}</td>
      <td>
        <div class="task-actions">
          <button class="btn btn-ghost btn-sm" type="button" data-open="${esc(task.id)}">Изменить</button>
          <button class="btn btn-danger-ghost btn-sm" type="button" data-delete="${esc(task.id)}">Удалить</button>
        </div>
      </td>
    </tr>
  `;
}

function sortHeader(label, key, filters) {
  const active = filters.sort === key;
  const arrow = active ? (filters.dir === 'asc' ? ' ↑' : ' ↓') : '';
  const aria = active
    ? ` aria-sort="${filters.dir === 'asc' ? 'ascending' : 'descending'}"`
    : '';
  return `<th scope="col"${aria}><button class="sort-btn" type="button" data-sort="${key}">${label}${arrow}</button></th>`;
}

function renderLoading() {
  root.innerHTML = `<div class="list-loading">${'<div class="skeleton"></div>'.repeat(6)}</div>`;
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
        : 'Создайте задачу, чтобы она появилась в списке и на доске.'}</p>
      <button class="btn btn-primary" type="button" data-action="${hasFilters ? 'reset' : 'add'}">
        ${hasFilters ? 'Сбросить фильтры' : 'Создать задачу'}
      </button>
    </div>
  `;
}

function onClick(event) {
  const open = event.target.closest('[data-open]');
  if (open) {
    handlers.onOpen?.(open.dataset.open);
    return;
  }

  const remove = event.target.closest('[data-delete]');
  if (remove) {
    const task = get('tasks').find((item) => item.id === remove.dataset.delete);
    if (task) handlers.onDelete?.(task);
    return;
  }

  const sort = event.target.closest('[data-sort]');
  if (sort) {
    handlers.onSort?.(sort.dataset.sort);
    return;
  }

  const action = event.target.closest('[data-action]');
  if (!action) return;

  if (action.dataset.action === 'retry') handlers.onRetry?.();
  else if (action.dataset.action === 'reset') handlers.onResetFilters?.();
  else if (action.dataset.action === 'reset-focus') handlers.onResetFocus?.();
  else if (action.dataset.action === 'add') handlers.onAdd?.('todo');
}

function onChange(event) {
  const select = event.target.closest('.status-select');
  if (select) handlers.onStatusChange?.(select.dataset.statusId, select.value);
}

export function init(container, options = {}) {
  root = container;
  handlers = options;
  root.addEventListener('click', onClick);
  root.addEventListener('change', onChange);
}

export function render() {
  if (!root) return;

  const allTasks = get('tasks');
  const filters = get('filters');
  const focusIds = get('focusIds');
  const loading = get('loading');
  const error = get('error');

  if (loading.tasks && !allTasks.length) {
    renderLoading();
    return;
  }

  if (error) {
    renderError(error);
    return;
  }

  const tasks = focusIds.length
    ? allTasks.filter((task) => focusIds.includes(task.id))
    : allTasks;

  const hasFilters = Boolean(filters.status || filters.priority || filters.search);
  if (!tasks.length) {
    renderEmpty(hasFilters);
    return;
  }

  const focusBar = focusIds.length
    ? `<div class="focus-bar">
        <span>Показаны задачи из подсказки: ${tasks.length}</span>
        <button class="btn btn-ghost btn-sm" type="button" data-action="reset-focus">Сбросить</button>
      </div>`
    : '';

  root.innerHTML = `
    ${focusBar}
    <div class="list-wrap">
      <table class="task-table">
        <thead>
          <tr>
            <th scope="col">Название</th>
            ${sortHeader('Приоритет', 'priority', filters)}
            ${sortHeader('Срок', 'due_at', filters)}
            <th scope="col">Статус</th>
            ${sortHeader('Создана', 'created_at', filters)}
            <th scope="col">Напоминания</th>
            <th scope="col">Действия</th>
          </tr>
        </thead>
        <tbody>${tasks.map(rowHtml).join('')}</tbody>
      </table>
    </div>
  `;
}
