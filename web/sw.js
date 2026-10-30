/**
 * Service Worker: показ push-уведомлений и фокус вкладки по клику.
 */
self.addEventListener('push', (event) => {
  let data = {};

  try {
    data = event.data?.json() ?? {};
  } catch {
    data = {};
  }

  event.waitUntil(
    self.registration.showNotification(data.title ?? 'Tracker', {
      body: data.body ?? '',
      tag: data.task_id ?? 'default',
      data: { url: '/' },
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windows) => {
      for (const windowClient of windows) {
        // Фокусируем вкладку приложения, а не страницу входа и не чужой origin.
        if (windowClient.url.startsWith(self.location.origin)) {
          return windowClient.focus();
        }
      }
      return clients.openWindow('/');
    }),
  );
});
