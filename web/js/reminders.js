/**
 * Напоминания внутри модалки задачи: список, создание и удаление.
 */
import { api } from './api.js';
import {
  clearFormErrors,
  esc,
  fmtDue,
  fmtLocalInputValue,
  fmtRelative,
  reminderStatusLabel,
  showFieldError,
  toast,
} from './ui.js';

const MAX_PENDING = 5;
const FIVE_YEARS_MS = 5 * 365 * 24 * 60 * 60 * 1000;

let root = null;
let task = null;
let onChange = () => {};

const pendingCount = () => (task?.reminders ?? []).filter((item) => item.status === 'pending').length;

function reminderItem(reminder) {
  return `
    <div class="reminder">
      <div class="reminder-main">
        <div class="reminder-time">${esc(fmtRelative(reminder.fire_at))} · ${esc(fmtDue(reminder.fire_at))}</div>
        <div class="reminder-message">${esc(reminder.message || 'Без текста')} · ${esc(reminderStatusLabel(reminder.status))}</div>
      </div>
      <button class="btn btn-ghost btn-sm" type="button" data-remove="${esc(reminder.id)}" aria-label="Удалить напоминание">×</button>
    </div>
  `;
}

function render() {
  if (!root) return;

  if (!task) {
    root.textContent = 'Сначала создайте задачу — напоминания добавляются к сохранённой задаче.';
    return;
  }

  const reminders = [...(task.reminders ?? [])].sort(
    (left, right) => new Date(left.fire_at) - new Date(right.fire_at),
  );
  const pending = pendingCount();
  const isDone = task.status === 'done';
  const isFull = pending >= MAX_PENDING;
  const disabled = isDone || isFull;

  root.innerHTML = `
    <p class="reminder-limit">Активных напоминаний: ${pending} из ${MAX_PENDING}${isDone ? ' · задача выполнена' : ''}</p>
    <div class="reminders">
      ${reminders.length ? reminders.map(reminderItem).join('') : '<p class="muted">Напоминаний пока нет.</p>'}
    </div>
    <form class="reminder-form" novalidate>
      <label class="field">Когда
        <input type="datetime-local" name="fire_at" min="${esc(fmtLocalInputValue(new Date().toISOString()))}" ${disabled ? 'disabled' : ''}>
        <span class="field-error" data-error="fire_at"></span>
      </label>
      <label class="field">Текст
        <input type="text" name="message" maxlength="200" placeholder="За час до дедлайна" ${disabled ? 'disabled' : ''}>
        <span class="field-error" data-error="message"></span>
      </label>
      <button class="btn btn-primary" type="submit" ${disabled ? 'disabled' : ''}>Сохранить</button>
    </form>
    ${isDone ? '<p class="muted">Для выполненной задачи напоминания не отправляются.</p>' : ''}
  `;

  const form = root.querySelector('.reminder-form');

  if (form) {
    const submit = form.querySelector('button[type="submit"]');
    const fireInput = form.elements.fire_at;

    const syncSubmit = () => {
      const date = fireInput.value ? new Date(fireInput.value) : null;
      const valid = date
        && !Number.isNaN(date.getTime())
        && date.getTime() >= Date.now() - 60000
        && date.getTime() <= Date.now() + FIVE_YEARS_MS;
      submit.disabled = disabled || !valid;
    };

    fireInput.addEventListener('input', syncSubmit);
    syncSubmit();

    form.addEventListener('submit', (event) => {
      event.preventDefault();
      createReminder(form);
    });
  }

  root.querySelectorAll('[data-remove]').forEach((button) => {
    button.addEventListener('click', () => removeReminder(button.dataset.remove));
  });
}

async function createReminder(form) {
  clearFormErrors(form);

  const fireAt = form.elements.fire_at.value;
  const message = form.elements.message.value.trim();

  if (!fireAt) {
    showFieldError(form, 'fire_at', 'Выберите дату и время');
    return;
  }

  const date = new Date(fireAt);
  if (Number.isNaN(date.getTime())) {
    showFieldError(form, 'fire_at', 'Неверная дата');
    return;
  }

  if (date.getTime() < Date.now() - 60000) {
    showFieldError(form, 'fire_at', 'Время уже прошло');
    return;
  }

  if (date.getTime() > Date.now() + FIVE_YEARS_MS) {
    showFieldError(form, 'fire_at', 'Слишком далёкая дата');
    return;
  }

  if (pendingCount() >= MAX_PENDING) {
    showFieldError(form, 'fire_at', `Не более ${MAX_PENDING} активных напоминаний`);
    return;
  }

  const optimistic = {
    id: `tmp-${Date.now()}`,
    task_id: task.id,
    fire_at: date.toISOString(),
    message,
    status: 'pending',
    created_at: new Date().toISOString(),
  };

  task.reminders = [...(task.reminders ?? []), optimistic];
  render();

  try {
    const saved = await api.createReminder(task.id, {
      fire_at: optimistic.fire_at,
      message,
    });
    task.reminders = (task.reminders ?? []).map((item) => (
      item.id === optimistic.id ? saved : item
    ));
    render();
    toast('Напоминание добавлено', 'success');
    onChange();
  } catch (error) {
    task.reminders = (task.reminders ?? []).filter((item) => item.id !== optimistic.id);
    render();

    const nextForm = root.querySelector('.reminder-form');
    if (nextForm && error.code === 'validation_failed') {
      const field = error.details?.[0]?.field ?? 'fire_at';
      showFieldError(nextForm, field, error.fieldError?.(field) ?? error.message);
    } else {
      toast(error.message || 'Не удалось добавить напоминание', 'error');
    }
  }
}

async function removeReminder(id) {
  const previous = task.reminders ?? [];
  task.reminders = previous.filter((item) => item.id !== id);
  render();

  try {
    await api.deleteReminder(id);
    onChange();
  } catch (error) {
    task.reminders = previous;
    render();
    toast(error.message || 'Не удалось удалить напоминание', 'error');
  }
}

export function init(container, options = {}) {
  root = container;
  task = options.task ?? null;
  onChange = options.onChange ?? (() => {});
  render();
}
