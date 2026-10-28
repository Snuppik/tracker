/**
 * Единое состояние приложения и подписки.
 * Один стор нужен, чтобы изменение задачи сразу обновляло доску,
 * список, счётчики и колокольчик.
 */
const state = {
  user: null,
  view: 'kanban',
  filters: { status: '', priority: '', search: '', sort: 'due_at', dir: 'asc' },
  tasks: [],
  total: 0,
  stats: null,
  insights: [],
  notifications: [],
  unread: 0,
  openTaskId: null,
  focusIds: [],
  loading: { tasks: false, insights: false },
  error: null,
};

const listeners = new Set();

function select(source, path) {
  return path.split('.').reduce((value, key) => (value == null ? value : value[key]), source);
}

export const get = (selector) => (selector ? structuredClone(select(state, selector)) : state);

export function set(patch) {
  Object.assign(state, patch);
  listeners.forEach((listener) => listener(state));
}

export function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
