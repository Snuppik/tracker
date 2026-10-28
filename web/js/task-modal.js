/**
 * Модалка задачи: создание, редактирование, удаление и вкладка напоминаний.
 */
import { api } from './api.js';
import * as reminders from './reminders.js';
import {
  clearFormErrors,
  confirmAction,
  fmtLocalInputValue,
  setBusy,
  showFieldError,
  toast,
  toISOFromLocal,
} from './ui.js';

let dialog = null;
let form = null;
let titleInput = null;
let descriptionInput = null;
let priorityInput = null;
let dueInput = null;
let statusInput = null;
let deleteButton = null;
let submitButton = null;
let titleEl = null;
let remindersRoot = null;
let tabs = [];
let panels = [];
let currentTask = null;
let handlers = {};

function switchTab(name) {
  tabs.forEach((tab) => {
    const active = tab.dataset.tab === name;
    tab.classList.toggle('is-active', active);
    tab.setAttribute('aria-selected', String(active));
  });

  panels.forEach((panel) => {
    panel.hidden = panel.dataset.panel !== name;
  });
}

/**
 * Сравнение двух меток времени: сервер отдаёт «…Z», toISOString добавляет
 * миллисекунды, поэтому строки сравнивать нельзя — только по таймстампу.
 */
function sameInstant(left, right) {
  if (!left && !right) return true;
  if (!left || !right) return false;
  return new Date(left).getTime() === new Date(right).getTime();
}

function applyError(error) {
  if (error.code === 'validation_failed' && error.details?.length) {
    error.details.forEach((detail) => {
      showFieldError(form, detail.field, detail.message);
    });
    return;
  }

  toast(error.message || 'Не удалось сохранить задачу', 'error');
}

function open(task = null, presetStatus = 'todo') {
  currentTask = task;
  form.reset();
  clearFormErrors(form);

  titleEl.textContent = task ? 'Задача' : 'Новая задача';
  submitButton.textContent = task ? 'Сохранить' : 'Создать задачу';
  deleteButton.hidden = !task;

  titleInput.value = task?.title ?? '';
  descriptionInput.value = task?.description ?? '';
  priorityInput.value = task?.priority ?? 'medium';
  dueInput.value = task?.due_at ? fmtLocalInputValue(task.due_at) : '';
  statusInput.value = task?.status ?? presetStatus;

  reminders.init(remindersRoot, {
    task,
    onChange: () => handlers.onRefresh?.(),
  });

  switchTab('details');
  if (!dialog.open) dialog.showModal();
  titleInput.focus();
}

export function init(container, options = {}) {
  dialog = container;
  handlers = options;

  form = dialog.querySelector('#taskForm');
  titleInput = dialog.querySelector('#taskTitle');
  descriptionInput = dialog.querySelector('#taskDescription');
  priorityInput = dialog.querySelector('#taskPriority');
  dueInput = dialog.querySelector('#taskDue');
  statusInput = dialog.querySelector('#taskStatus');
  deleteButton = dialog.querySelector('#deleteTask');
  submitButton = dialog.querySelector('#saveTask');
  titleEl = dialog.querySelector('#taskDialogTitle');
  remindersRoot = dialog.querySelector('#reminders');
  tabs = [...dialog.querySelectorAll('[data-tab]')];
  panels = [...dialog.querySelectorAll('[data-panel]')];

  dialog.querySelectorAll('[data-close]').forEach((button) => {
    button.addEventListener('click', () => dialog.close());
  });

  tabs.forEach((tab) => {
    tab.addEventListener('click', () => switchTab(tab.dataset.tab));
  });

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    clearFormErrors(form);

    const title = titleInput.value.trim();
    if (!title) {
      showFieldError(form, 'title', 'Введите название');
      titleInput.focus();
      return;
    }

    const dueISO = toISOFromLocal(dueInput.value);
    const edited = currentTask;

    const payload = {
      title,
      description: descriptionInput.value.trim(),
      priority: priorityInput.value,
    };

    // PATCH: due_at без поля = «не менять», due_at: null = «очистить срок».
    if (!edited || !sameInstant(edited.due_at, dueISO)) {
      payload.due_at = dueISO;
    }

    setBusy(submitButton, true);

    try {
      const saved = edited
        ? await api.patchTask(edited.id, payload)
        : await api.createTask({ ...payload, status: statusInput.value });

      dialog.close();
      currentTask = null;
      toast(edited ? 'Изменения сохранены' : 'Задача создана', 'success');
      handlers.onSaved?.(saved, Boolean(edited));
    } catch (error) {
      applyError(error);
    } finally {
      setBusy(submitButton, false);
    }
  });

  deleteButton.addEventListener('click', async () => {
    if (!currentTask) return;

    const task = currentTask;
    const confirmed = await confirmAction({
      title: `Удалить «${task.title}»?`,
      text: 'Задача и её напоминания будут удалены без возможности восстановления.',
      confirmLabel: 'Удалить задачу',
    });

    if (!confirmed) return;

    try {
      await api.deleteTask(task.id);
      dialog.close();
      currentTask = null;
      toast('Задача удалена', 'success');
      handlers.onDeleted?.(task.id);
    } catch (error) {
      toast(error.message || 'Не удалось удалить задачу', 'error');
    }
  });

  statusInput.addEventListener('change', async () => {
    if (!currentTask) return;

    const previous = currentTask.status;
    const next = statusInput.value;
    currentTask = { ...currentTask, status: next };

    const changed = await handlers.onStatusChange?.(currentTask.id, next);

    if (changed === false) {
      currentTask = { ...currentTask, status: previous };
      statusInput.value = previous;
      return;
    }

    if (next === 'done') {
      // Сервер в той же транзакции гасит pending-напоминания задачи.
      currentTask = {
        ...currentTask,
        reminders: (currentTask.reminders ?? []).map((item) => (
          item.status === 'pending' ? { ...item, status: 'expired' } : item
        )),
      };
    }

    reminders.init(remindersRoot, {
      task: currentTask,
      onChange: () => handlers.onRefresh?.(),
    });
  });

  dialog.addEventListener('close', () => {
    clearFormErrors(form);
  });
}

export { open };
