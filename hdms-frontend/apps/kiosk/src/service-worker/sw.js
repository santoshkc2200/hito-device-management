// Replace the existing /sw.js caching worker on staging. New pages do not
// register this cleanup worker again.
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
      .filter((client) => client.url.startsWith(scope)
        && !/^\/(admin|staff|recovery)(\/|$)/.test(new URL(client.url).pathname))
      .map((client) => client.navigate(client.url)));
  })());
});
