// Pass-through service worker. It exists so Richmod is installable; it keeps no
// cache and intercepts no request. HTML, chunks, the manifest and the API all
// come from the network exactly as they would without a worker, so no release
// can leave an installed client on a build whose chunks the server deleted.
// Offline use is not supported.
//
// Older releases cached /_next/static and kept richmod-generations metadata;
// activate removes those now-unused stores so they do not hold storage forever.
self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", event => {
  event.waitUntil((async () => {
    try {
      for (const key of await caches.keys()) {
        if (key.startsWith("richmod-static-") || key === "richmod-generations") await caches.delete(key);
      }
    } catch {
      // Cache Storage may be unavailable; activation must still complete.
    }
    await self.clients.claim();
  })());
});
