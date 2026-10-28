/**
 * Состояние вида и фильтров в URL.
 * Позволяет пережить F5 и показать «вот мой фильтр» повторной загрузкой.
 */
import { get, set } from './state.js';

const STATUSES = ['todo', 'in_progress', 'done'];
const PRIORITIES = ['low', 'medium', 'high'];
const SORTS = ['due_at', 'created_at', 'priority'];

export function read() {
  const params = new URLSearchParams(location.search);
  const status = params.get('status') ?? '';
  const priority = params.get('priority') ?? '';
  const sort = params.get('sort') ?? 'due_at';

  return {
    view: params.get('view') === 'list' ? 'list' : 'kanban',
    filters: {
      status: STATUSES.includes(status) ? status : '',
      priority: PRIORITIES.includes(priority) ? priority : '',
      search: params.get('q') ?? '',
      sort: SORTS.includes(sort) ? sort : 'due_at',
      dir: params.get('dir') === 'desc' ? 'desc' : 'asc',
    },
  };
}

export function write() {
  const { view, filters } = get();
  const params = new URLSearchParams();

  if (view === 'list') params.set('view', 'list');
  if (filters.status) params.set('status', filters.status);
  if (filters.priority) params.set('priority', filters.priority);
  if (filters.search) params.set('q', filters.search);
  if (filters.sort !== 'due_at') params.set('sort', filters.sort);
  if (filters.dir !== 'asc') params.set('dir', filters.dir);

  const query = params.toString();
  history.replaceState(null, '', query ? `?${query}` : location.pathname);
}

export function setFilters(patch) {
  set({ filters: { ...get('filters'), ...patch } });
  write();
}

export function setView(view) {
  set({ view: view === 'list' ? 'list' : 'kanban' });
  write();
}
