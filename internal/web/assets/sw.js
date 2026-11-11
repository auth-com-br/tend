// tend web's service worker: shows the notifications the server pushes
// (push.go) with the page closed, and opens the agent a tap is about.
'use strict';

self.addEventListener('push', e => {
  let n = {};
  try {
    n = e.data ? e.data.json() : {};
  } catch {
    n = { title: 'tend', body: e.data ? e.data.text() : '' };
  }
  e.waitUntil(
    self.registration.showNotification(n.title || 'tend', {
      body: n.body || '',
      tag: n.tag,
      renotify: Boolean(n.tag),
      icon: '/icon.svg',
      badge: '/icon.svg',
      vibrate: [200, 100, 200],
      data: { pane: n.pane || 0 },
    }),
  );
});

self.addEventListener('notificationclick', e => {
  e.notification.close();
  const pane = e.notification.data && e.notification.data.pane;
  const url = '/' + (pane ? `?pane=${pane}` : '');
  e.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(list => {
      for (const c of list) {
        if ('focus' in c) {
          return c.navigate(url).then(w => (w || c).focus());
        }
      }
      return self.clients.openWindow(url);
    }),
  );
});
