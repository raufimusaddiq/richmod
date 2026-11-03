// Service worker template. scripts/build-service-worker.mjs stamps the BUILD
// constant with a hash of the exported _next/static tree and writes out/sw.js,
// which webui.go serves with Cache-Control: no-cache.
//
// Strategy, per route class:
//   - Navigations and .txt RSC payloads: never intercepted (network-only). A
//     cached HTML shell is exactly what strands an installed client on a build
//     whose chunks the next release deleted.
//   - /api, /webhooks, /finance, /healthz, /readyz: never intercepted.
//   - /_next/static/* : cache-first. Next hashes these filenames, so a cached
//     entry can never be the wrong bytes for its URL.
//   - everything else (manifest, icons, fonts, brand art): network-only.
// There is no precached shell. Offline browsing is deliberately out of scope.
const BUILD = "__BUILD_ID__";
const CACHE = `richmod-static-${BUILD}`;

self.addEventListener("install", () => {
  // Take over on the next launch instead of waiting for every tab to close: the
  // cache name is build-scoped and HTML is never served from cache, so an
  // immediate activation cannot hand out a stale shell.
  self.skipWaiting();
});

self.addEventListener("activate", event => {
  event.waitUntil((async () => {
    for (const key of await caches.keys()) {
      if (key.startsWith("richmod-static-") && key !== CACHE) await caches.delete(key);
    }
    await self.clients.claim();
  })());
});

self.addEventListener("fetch", event => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  // Static chunks only. Everything else stays on the network so a release is
  // visible on the next load, including deep links into the app shell.
  if (!url.pathname.startsWith("/_next/static/")) return;
  event.respondWith(cacheFirst(request));
});

async function cacheFirst(request) {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(request);
  if (hit) return hit;
  const response = await fetch(request);
  if (response.ok) await cache.put(request, response.clone());
  return response;
}
