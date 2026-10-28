/**
 * Единственное место, где выполняется fetch.
 * Все запросы идут на тот же origin с cookie tracker_session.
 */
async function request(path, { method = 'GET', body, query } = {}) {
  const url = new URL(path, location.origin);
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value !== null && value !== undefined && value !== '') {
        url.searchParams.set(key, value);
      }
    }
  }

  let res;
  try {
    res = await fetch(url, {
      method,
      credentials: 'same-origin',
      headers: body ? { 'Content-Type': 'application/json' } : {},
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError('network', 'Нет связи с сервером', []);
  }

  if (res.status === 401 && !path.startsWith('/api/auth/')) {
    location.href = '/login';
    throw new ApiError('unauthorized', 'Сессия истекла', []);
  }

  if (res.status === 204) return null;

  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(
      data.error?.code ?? 'internal',
      data.error?.message ?? 'Ошибка сервера',
      data.error?.details ?? [],
    );
  }

  return data;
}

export const api = {
  register(input) {
    return request('/api/auth/register', { method: 'POST', body: input });
  },
  login(input) {
    return request('/api/auth/login', { method: 'POST', body: input });
  },
  logout() {
    return request('/api/auth/logout', { method: 'POST' });
  },
  me() {
    return request('/api/auth/me');
  },

  listTasks(filters) {
    return request('/api/tasks', { query: filters });
  },
  getTask(id) {
    return request(`/api/tasks/${id}`);
  },
  createTask(input) {
    return request('/api/tasks', { method: 'POST', body: input });
  },
  patchTask(id, patch) {
    return request(`/api/tasks/${id}`, { method: 'PATCH', body: patch });
  },
  setStatus(id, status) {
    return request(`/api/tasks/${id}/status`, { method: 'PUT', body: { status } });
  },
  deleteTask(id) {
    return request(`/api/tasks/${id}`, { method: 'DELETE' });
  },

  createReminder(taskId, reminder) {
    return request(`/api/tasks/${taskId}/reminders`, { method: 'POST', body: reminder });
  },
  deleteReminder(id) {
    return request(`/api/reminders/${id}`, { method: 'DELETE' });
  },

  stats() {
    return request('/api/stats');
  },
  insights() {
    return request('/api/insights');
  },
  notifications(unread = false) {
    return request('/api/notifications', { query: { unread } });
  },
  markRead(id) {
    return request(`/api/notifications/${id}/read`, { method: 'POST' });
  },
  markAllRead() {
    return request('/api/notifications/read-all', { method: 'POST' });
  },

  vapidKey() {
    return request('/api/push/public-key');
  },
  putPush(subscription) {
    return request('/api/push/subscription', { method: 'PUT', body: subscription });
  },
  delPush(endpoint) {
    return request('/api/push/subscription', { method: 'DELETE', body: { endpoint } });
  },
};

export class ApiError extends Error {
  constructor(code, message, details) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.details = details;
  }

  /** Сообщение для конкретного поля формы */
  fieldError(field) {
    return this.details.find((detail) => detail.field === field)?.message;
  }
}
