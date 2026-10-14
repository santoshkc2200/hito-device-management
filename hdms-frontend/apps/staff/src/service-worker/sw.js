
// Published at the existing /staff/sw.js URL so installed caching workers
// discover this replacement. New staging pages do not register it again.
self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const scope = self.registration.scope;
    const cacheNames = await self.caches.keys();
    await Promise.all(cacheNames
      .filter((name) => name.startsWith("workbox-") && name.endsWith(scope))
      .map((name) => self.caches.delete(name)));
    const windows = await self.clients.matchAll({ type: "window" });
    await self.registration.unregister();
    await Promise.all(windows
      .filter((client) => client.url.startsWith(scope))
      .map((client) => client.navigate(client.url)));
  })());
});
