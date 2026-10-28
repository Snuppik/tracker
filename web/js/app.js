/**
 * Bootstrap приложения: вход/регистрация, проверка сессии,
 * связывание модулей и обработчики шапки.
 */
import { api } from './api.js';
import { get, set, subscribe } from './state.js';
import {
  clearFormErrors,
  confirmAction,
  debounce,
  formatToday,
  setBusy,
  showFieldError,
  toast,
} from './ui.js';
import * as router from './router.js';
import * as dashboard from './dashboard.js';
import * as insights from './insights.js';
import * as kanban from './kanban.js';
import * as list from './list.js';
import * as taskModal from './task-modal.js';
import * as notifications from './notifications.js';

const THEME_KEY = 'tracker_theme';

function applyTheme(theme) {
  const next = theme === 'dark' ? 'dark' : 'light';
  document.documentElement.dataset.theme = next;

  try {
    localStorage.setItem(THEME_KEY, next);
  } catch {
    // Приватный режим: тема просто не сохранится.
  }

  document.querySelectorAll('[data-theme-toggle]').forEach((button) => {
    button.textContent = next === 'dark' ? '☀ Светлая тема' : '☾ Тёмная тема';
  });
}

function initTheme() {
  let saved = 'light';
  try {
    saved = localStorage.getItem(THEME_KEY) ?? 'light';
  } catch {
    saved = 'light';
  }

  applyTheme(saved);

  document.querySelectorAll('[data-theme-toggle]').forEach((button) => {
    button.addEventListener('click', () => {
      applyTheme(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark');
    });
  });
}

function initLogin() {
  const form = document.getElementById('loginForm');
  const nameField = document.getElementById('nameField');
  const nameInput = document.getElementById('name');
  const emailInput = document.getElementById('email');
  const passwordInput = document.getElementById('password');
  const submitButton = document.getElementById('submitAuth');
  const formError = document.getElementById('formError');
  const formTitle = document.getElementById('formTitle');
  const formHint = document.getElementById('formHint');

  let mode = 'login';

  const switchMode = (next) => {
    mode = next;
    const isRegister = next === 'register';

    nameField.hidden = !isRegister;
    nameInput.required = isRegister;
    passwordInput.minLength = isRegister ? 8 : 0;
    passwordInput.autocomplete = isRegister ? 'new-password' : 'current-password';
    formTitle.textContent = isRegister ? 'Создайте аккаунт' : 'Начните свой путь';
    formHint.textContent = isRegister
      ? 'Одна регистрация — и задачи всегда под рукой.'
      : 'Войдите, чтобы вернуться к своим задачам.';
    submitButton.textContent = isRegister ? 'Зарегистрироваться →' : 'Войти →';
    formError.hidden = true;

    clearFormErrors(form);

    document.querySelectorAll('[data-mode]').forEach((button) => {
      button.setAttribute('aria-selected', String(button.dataset.mode === next));
    });
  };

  document.querySelectorAll('[data-mode]').forEach((button) => {
    button.addEventListener('click', () => switchMode(button.dataset.mode));
  });

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    clearFormErrors(form);
    formError.hidden = true;

    const email = emailInput.value.trim();
    const password = passwordInput.value;
    const name = nameInput.value.trim();

    if (!email) {
      showFieldError(form, 'email', 'Введите email');
      emailInput.focus();
      return;
    }

    if (!password) {
      showFieldError(form, 'password', 'Введите пароль');
      passwordInput.focus();
      return;
    }

    if (mode === 'register' && !name) {
      showFieldError(form, 'name', 'Введите имя');
      nameInput.focus();
      return;
    }

    const payload = { email, password };
    if (mode === 'register') payload.name = name;

    setBusy(submitButton, true);

    try {
      if (mode === 'register') await api.register(payload);
      else await api.login(payload);

      location.href = '/';
    } catch (error) {
      if (error.code === 'validation_failed' && error.details?.length) {
        error.details.forEach((detail) => {
          showFieldError(form, detail.field, detail.message);
        });
      } else {
        formError.textContent = error.code === 'conflict'
          ? 'Этот email уже зарегистрирован'
          : error.message;
        formError.hidden = false;
      }
    } finally {
      setBusy(submitButton, false);
    }
  });

  switchMode('login');
}

function initApp() {
  const board = document.getElementById('board');
  const listRoot = document.getElementById('list');
  const statsRoot = document.getElementById('stats');
  const insightsRoot = document.getElementById('insights');
  const searchInput = document.getElementById('search');
  const statusFilter = document.getElementById('filterStatus');
  const priorityFilter = document.getElementById('filterPriority');
  const resultCount = document.getElementById('resultCount');
  const todayEl = document.getElementById('today');
  const userName = document.getElementById('userName');

  const { view, filters } = router.read();
  set({ view, filters });

  dashboard.init(statsRoot);
  insights.init(insightsRoot, { onShowTasks });
  kanban.init(board, {
    onOpen: openTask,
    onAdd: (status) => openTask(null, status),
    onStatusChange: changeStatus,
    onRetry: loadTasks,
    onResetFilters: resetFilters,
  });
  list.init(listRoot, {
    onOpen: openTask,
    onDelete: deleteTask,
    onStatusChange: changeStatus,
    onSort: sortBy,
    onResetFocus: resetFocus,
    onRetry: loadTasks,
    onResetFilters: resetFilters,
    onAdd: (status) => openTask(null, status),
  });
  taskModal.init(document.getElementById('taskDialog'), {
    onSaved: afterTaskSaved,
    onDeleted: afterTaskDeleted,
    onRefresh: refreshAll,
    onStatusChange: changeStatus,
  });
  notifications.init({ onOpenTask: openTask });

  subscribe(renderAll);

  if (todayEl) todayEl.textContent = formatToday();

  syncFilterInputs();
  syncViewButtons();

  searchInput?.addEventListener('input', debounce(() => {
    updateFilters({ search: searchInput.value.trim() });
  }, 300));

  statusFilter?.addEventListener('change', () => updateFilters({ status: statusFilter.value }));
  priorityFilter?.addEventListener('change', () => updateFilters({ priority: priorityFilter.value }));

  document.querySelectorAll('[data-view]').forEach((button) => {
    button.addEventListener('click', () => {
      const next = button.dataset.view;
      if (next !== 'kanban' && next !== 'list') return;
      set({ view: next, focusIds: [] });
      router.write();
    });
  });

  document.querySelectorAll('[data-add]').forEach((button) => {
    button.addEventListener('click', () => openTask(null, 'todo'));
  });

  document.getElementById('logout')?.addEventListener('click', async () => {
    try {
      await api.logout();
    } catch {
      // Выходим локально в любом случае.
    }
    location.href = '/login';
  });

  function syncViewButtons() {
    const current = get('view');

    document.querySelectorAll('[data-view]').forEach((button) => {
      const active = button.dataset.view === current;
      if (button.classList.contains('nav-link')) {
        if (active) button.setAttribute('aria-current', 'page');
        else button.removeAttribute('aria-current');
      } else {
        button.setAttribute('aria-pressed', String(active));
      }
    });
  }

  function syncFilterInputs() {
    const current = get('filters');
    if (searchInput) searchInput.value = current.search;
    if (statusFilter) statusFilter.value = current.status;
    if (priorityFilter) priorityFilter.value = current.priority;
  }

  function updateResultCount() {
    if (!resultCount) return;

    const { total, loading, focusIds } = get();
    if (focusIds.length) {
      resultCount.textContent = `Из подсказки: ${focusIds.length}`;
    } else if (loading.tasks) {
      resultCount.textContent = 'Загрузка…';
    } else {
      resultCount.textContent = `Найдено: ${total}`;
    }
  }

  function renderAll() {
    const current = get('view');
    if (board) board.hidden = current !== 'kanban';
    if (listRoot) listRoot.hidden = current !== 'list';

    dashboard.render();
    insights.render();
    kanban.render();
    list.render();
    notifications.render();
    syncViewButtons();
    updateResultCount();
  }

  function updateFilters(patch) {
    set({ filters: { ...get('filters'), ...patch }, focusIds: [] });
    router.write();
    loadTasks();
  }

  function resetFilters() {
    set({
      filters: { status: '', priority: '', search: '', sort: 'due_at', dir: 'asc' },
      focusIds: [],
    });
    syncFilterInputs();
    router.write();
    loadTasks();
  }

  function resetFocus() {
    set({ focusIds: [] });
  }

  function sortBy(key) {
    const { sort, dir } = get('filters');
    const nextDir = sort === key && dir === 'asc' ? 'desc' : 'asc';
    updateFilters({ sort: key, dir: nextDir });
  }

  function onShowTasks(ids) {
    set({
      view: 'list',
      focusIds: ids,
      filters: { ...get('filters'), status: '', priority: '', search: '' },
    });
    syncFilterInputs();
    router.write();
    loadTasks();
  }

  async function loadTasks() {
    set({ loading: { ...get('loading'), tasks: true }, error: null });

    try {
      const data = await api.listTasks({ ...get('filters'), limit: 200 });
      set({
        tasks: data.items,
        total: data.total,
        loading: { ...get('loading'), tasks: false },
      });
    } catch (error) {
      set({
        loading: { ...get('loading'), tasks: false },
        error: error.message || 'Не удалось загрузить задачи',
      });
    }
  }

  async function loadStats() {
    try {
      const stats = await api.stats();
      set({ stats });
    } catch {
      // Счётчики обновятся при следующем запросе.
    }
  }

  async function loadInsights() {
    set({ loading: { ...get('loading'), insights: true } });

    try {
      const data = await api.insights();
      set({
        insights: data.items,
        loading: { ...get('loading'), insights: false },
      });
    } catch {
      set({ loading: { ...get('loading'), insights: false } });
    }
  }

  async function refreshAll() {
    await Promise.all([loadTasks(), loadStats(), loadInsights()]);
  }

  async function changeStatus(id, status) {
    const previous = get('tasks');
    const task = previous.find((item) => item.id === id);

    if (!task || task.status === status) return true;

    set({
      tasks: previous.map((item) => (item.id === id ? { ...item, status } : item)),
    });

    try {
      const updated = await api.setStatus(id, status);
      set({ tasks: get('tasks').map((item) => (item.id === id ? updated : item)) });
      toast('Статус обновлён', 'success');
      loadStats();
      loadInsights();
      return true;
    } catch (error) {
      set({ tasks: previous });
      toast(error.message || 'Не удалось изменить статус', 'error');
      return false;
    }
  }

  async function openTask(id = null, presetStatus = 'todo') {
    if (!id) {
      taskModal.open(null, presetStatus);
      return;
    }

    try {
      const task = await api.getTask(id);
      taskModal.open(task);
    } catch (error) {
      toast(error.message || 'Не удалось открыть задачу', 'error');
    }
  }

  async function deleteTask(task) {
    const confirmed = await confirmAction({
      title: `Удалить «${task.title}»?`,
      text: 'Задача и её напоминания будут удалены без возможности восстановления.',
      confirmLabel: 'Удалить задачу',
    });

    if (!confirmed) return;

    try {
      await api.deleteTask(task.id);
      set({
        tasks: get('tasks').filter((item) => item.id !== task.id),
        total: Math.max(0, get('total') - 1),
      });
      toast('Задача удалена', 'success');
      refreshAll();
    } catch (error) {
      toast(error.message || 'Не удалось удалить задачу', 'error');
    }
  }

  function afterTaskSaved(saved, edited) {
    const tasks = get('tasks');
    const exists = tasks.some((item) => item.id === saved.id);

    set({
      tasks: exists
        ? tasks.map((item) => (item.id === saved.id ? saved : item))
        : [saved, ...tasks],
      total: exists ? get('total') : get('total') + 1,
    });

    refreshAll();
  }

  function afterTaskDeleted(id) {
    set({
      tasks: get('tasks').filter((item) => item.id !== id),
      total: Math.max(0, get('total') - 1),
    });
    refreshAll();
  }

  async function boot() {
    try {
      const { user } = await api.me();
      set({ user });
      if (userName) userName.textContent = user.name;
    } catch {
      location.replace('/login');
      return;
    }

    await refreshAll();
    notifications.poll();
  }

  boot();
}

initTheme();

if (document.getElementById('loginForm')) {
  initLogin();
} else {
  initApp();
}
