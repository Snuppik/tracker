/**
 * Колокольчик, лента уведомлений, polling и подписка на Web Push.
 */
import { api } from './api.js';
import { get, set } from './state.js';
import { esc, fmtAgo, toast } from './ui.js';

const POLL_MS = 30000;

let listEl = null;
let badgeEl = null;
let dialog = null;
let handlers = {};
let timer = null;

function render() {
  if (!listEl || !badgeEl) return;

  const notifications = get('notifications');
  const unread = get('unread');

  badgeEl.hidden = unread === 0;
  badgeEl.textContent = unread > 99 ? '99+' : String(unread);

  if (!notifications.length) {
    listEl.innerHTML = `
      <div class="empty">
        <div class="empty-art" aria-hidden="true">🔔</div>
        <h3>Уведомлений пока нет</h3>
        <p>Здесь появятся напоминания по вашим задачам.</p>
      </div>
    `;
    return;
  }

  listEl.innerHTML = notifications.map((item) => `
    <button class="notification${item.read ? '' : ' is-unread'}" type="button"
      data-id="${esc(item.id)}" data-task="${esc(item.task_id ?? '')}">
      <span class="notification-title">${esc(item.title)}</span>
      <span class="notification-body">${esc(item.body)}</span>
      <span class="notification-time">${esc(fmtAgo(item.created_at))}</span>
    </button>
  `).join('');

  listEl.querySelectorAll('.notification').forEach((button) => {
    button.addEventListener('click', () => openNotification(button.dataset.id, button.dataset.task));
  });
}

async function openNotification(id, taskId) {
  const notification = get('notifications').find((item) => item.id === id);

  if (notification && !notification.read) {
    try {
      await api.markRead(id);
      set({
        notifications: get('notifications').map((item) => (
          item.id === id ? { ...item, read: true } : item
        )),
        unread: Math.max(0, get('unread') - 1),
      });
    } catch {
      // Лента остаётся рабочей даже если отметка не сохранилась.
    }
  }

  if (taskId) {
    dialog?.close();
    handlers.onOpenTask?.(taskId);
  }
}

async function markAllRead() {
  try {
    await api.markAllRead();
    set({
      notifications: get('notifications').map((item) => ({ ...item, read: true })),
      unread: 0,
    });
    toast('Все уведомления прочитаны', 'success');
  } catch (error) {
    toast(error.message || 'Не удалось отметить уведомления', 'error');
  }
}

async function poll() {
  try {
    const data = await api.notifications();
    set({ notifications: data.items, unread: data.unread });
  } catch {
    // Молча: следующая попытка через 30 секунд.
  }
}

function stopPolling() {
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
}

function startPolling() {
  stopPolling();
  timer = setInterval(poll, POLL_MS);
}

function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  return Uint8Array.from([...raw].map((char) => char.charCodeAt(0)));
}

async function enablePush() {
  if (!('Notification' in window) || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    toast('Push недоступен: нужен localhost или HTTPS', 'error');
    return;
  }

  const permission = await Notification.requestPermission();
  if (permission !== 'granted') {
    toast('Разрешение на уведомления не выдано', 'error');
    return;
  }

  try {
    const { public_key: publicKey } = await api.vapidKey();
    const registration = await navigator.serviceWorker.ready;
    let subscription = await registration.pushManager.getSubscription();

    if (!subscription) {
      subscription = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(publicKey),
      });
    }

    await api.putPush(subscription.toJSON());
    toast('Уведомления включены', 'success');
  } catch (error) {
    toast(error.message || 'Не удалось включить уведомления', 'error');
  }
}

async function registerServiceWorker() {
  if (!('serviceWorker' in navigator)) return;

  try {
    const registration = await navigator.serviceWorker.register('/sw.js');
    if (!('pushManager' in registration)) return;

    const subscription = await registration.pushManager.getSubscription();
    if (subscription) {
      await api.putPush(subscription.toJSON());
    }
  } catch {
    // Push — надстройка: лента уведомлений работает и без него.
  }
}

export function init(options = {}) {
  handlers = options;
  listEl = document.getElementById('notificationsList');
  badgeEl = document.getElementById('unread');
  dialog = document.getElementById('notificationsDialog');

  const bell = document.getElementById('bell');
  const markAllButton = document.getElementById('markAllRead');
  const enablePushButton = document.getElementById('enablePush');

  bell?.addEventListener('click', () => {
    render();
    dialog?.showModal();
  });

  dialog?.querySelectorAll('[data-close]').forEach((button) => {
    button.addEventListener('click', () => dialog.close());
  });

  markAllButton?.addEventListener('click', markAllRead);
  enablePushButton?.addEventListener('click', enablePush);

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      stopPolling();
    } else {
      startPolling();
      poll();
    }
  });

  startPolling();
  registerServiceWorker();
}

export { render, poll };
