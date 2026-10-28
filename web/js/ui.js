/**
 * UI-утилиты: экранирование, форматирование дат, тосты, диалоги.
 */

const timeFmt = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' });
const dayFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short' });
const fullFmt = new Intl.DateTimeFormat('ru-RU', {
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
});
const todayFmt = new Intl.DateTimeFormat('ru-RU', {
  weekday: 'long',
  day: 'numeric',
  month: 'long',
});

/** Экранирование пользовательских строк для шаблонных строк. */
export const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}[char]));

const toDate = (iso) => {
  if (!iso) return null;
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? null : date;
};

const startOfDay = (date) => new Date(date.getFullYear(), date.getMonth(), date.getDate());

/** «сегодня, 18:30», «завтра, 12:00», «29 окт, 18:30». */
export function fmtDue(iso) {
  const date = toDate(iso);
  if (!date) return 'Без срока';

  const days = Math.round((startOfDay(date) - startOfDay(new Date())) / 86400000);
  const time = timeFmt.format(date);

  if (days === 0) return `сегодня, ${time}`;
  if (days === 1) return `завтра, ${time}`;
  return `${dayFmt.format(date)}, ${time}`;
}

/** «через 2 ч», «просрочено на 15 мин». */
export function fmtRelative(iso) {
  const date = toDate(iso);
  if (!date) return '';

  const diff = date.getTime() - Date.now();
  const minutes = Math.round(Math.abs(diff) / 60000);

  if (minutes < 1) return diff >= 0 ? 'меньше чем через минуту' : 'только что';
  if (minutes < 60) return diff >= 0 ? `через ${minutes} мин` : `просрочено на ${minutes} мин`;

  const hours = Math.round(minutes / 60);
  if (hours < 24) return diff >= 0 ? `через ${hours} ч` : `просрочено на ${hours} ч`;

  const days = Math.round(hours / 24);
  return diff >= 0 ? `через ${days} дн.` : `просрочено на ${days} дн.`;
}

/** «2 мин назад» — для ленты уведомлений. */
export function fmtAgo(iso) {
  const date = toDate(iso);
  if (!date) return '';

  const minutes = Math.round((Date.now() - date.getTime()) / 60000);
  if (minutes < 1) return 'только что';
  if (minutes < 60) return `${minutes} мин назад`;

  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} ч назад`;

  const days = Math.round(hours / 24);
  return `${days} дн. назад`;
}

/** Значение для input[type=datetime-local] в локальной зоне. */
export function fmtLocalInputValue(iso) {
  const date = toDate(iso);
  if (!date) return '';
  const pad = (value) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** Локальное значение datetime-local → RFC3339 UTC. */
export function toISOFromLocal(value) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

export function fmtDateTime(iso) {
  const date = toDate(iso);
  return date ? fullFmt.format(date) : '';
}

export function formatToday() {
  return todayFmt.format(new Date());
}

export const statusLabel = (status) => ({
  todo: 'К работе',
  in_progress: 'В работе',
  done: 'Готово',
}[status] ?? status);

export const priorityLabel = (priority) => ({
  low: 'Низкий',
  medium: 'Средний',
  high: 'Высокий',
}[priority] ?? priority);

export const reminderStatusLabel = (status) => ({
  pending: 'ожидает',
  sent: 'отправлено',
  expired: 'истекло',
  failed: 'ошибка',
}[status] ?? status);

export const severityIcon = (severity) => ({
  info: 'ℹ️',
  warning: '⚠️',
  critical: '🚨',
}[severity] ?? 'ℹ️');

/** Тосты: 3 секунды, стек справа сверху. */
export function toast(message, type = 'info') {
  const root = document.getElementById('toasts');
  if (!root) return;

  const item = document.createElement('div');
  item.className = `toast${type === 'success' ? ' is-success' : type === 'error' ? ' is-error' : ''}`;
  item.textContent = message;
  root.append(item);

  setTimeout(() => {
    item.classList.add('is-hiding');
    setTimeout(() => item.remove(), 300);
  }, 3000);
}

export function debounce(fn, wait = 300) {
  let timer = null;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), wait);
  };
}

/** Подтверждение через нативный <dialog>. Возвращает Promise<boolean>. */
export function confirmAction({ title, text = '', confirmLabel = 'Удалить' }) {
  const dialog = document.getElementById('confirmDialog');
  const titleEl = document.getElementById('confirmTitle');
  const textEl = document.getElementById('confirmText');
  const okButton = document.getElementById('confirmOk');

  if (!dialog || !titleEl || !textEl || !okButton) return Promise.resolve(false);

  titleEl.textContent = title;
  textEl.textContent = text;
  okButton.textContent = confirmLabel;

  if (dialog.open) dialog.close('cancel');
  dialog.returnValue = '';
  dialog.showModal();

  return new Promise((resolve) => {
    const onClose = () => {
      dialog.removeEventListener('close', onClose);
      resolve(dialog.returnValue === 'ok');
    };
    dialog.addEventListener('close', onClose);
  });
}

export function clearFormErrors(form) {
  form.querySelectorAll('[data-error]').forEach((slot) => {
    slot.textContent = '';
  });
  form.querySelectorAll('[aria-invalid="true"]').forEach((input) => {
    input.removeAttribute('aria-invalid');
  });
}

export function showFieldError(form, field, message) {
  const slot = form.querySelector(`[data-error="${field}"]`);
  if (slot) slot.textContent = message;

  const input = form.querySelector(`[name="${field}"]`);
  if (input) input.setAttribute('aria-invalid', 'true');
}

export function setBusy(button, busy) {
  if (!button) return;
  button.disabled = busy;
  button.setAttribute('aria-busy', String(busy));
}
