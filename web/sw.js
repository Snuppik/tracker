// Service Worker: показывает push-уведомление и фокусирует вкладку по клику.
// Заглушка до готовности фронтенда (docs/06-reminders-push.md §4.2).
self.addEventListener('push', (event) => {
  const data = event.data?.json() ?? {};
  event.waitUntil(
    self.registration.showNotification(data.title ?? 'Tracker', {
      body: data.body ?? '',
      tag: data.task_id ?? 'default',
      data: { url: '/' },
    })
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil(
    clients.matchAll({ type: 'window' }).then((wins) => {
      for (const w of wins) {
        if (w.url.includes('/')) {
          w.focus();
          return;
        }
      }
      return clients.openWindow('/');
    })
  );
});
