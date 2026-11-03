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
const STATIC_PREFIX = "richmod-static-";
const GENERATIONS = "richmod-generations";

self.addEventListener("install", () => {
  // Take over on the next launch instead of waiting for every tab to close: the
  // cache name is build-scoped and HTML is never served from cache, so an
  // immediate activation cannot hand out a stale shell.
  self.skipWaiting();
});

self.addEventListener("activate", event => {
  event.waitUntil((async () => {
    try {
      await retainPreviousGeneration();
    } catch {
      // Pruning is housekeeping. If Cache Storage refuses, keep serving rather
      // than failing activation.
    }
    await self.clients.claim();
  })());
});

// Keep this build and the one before it. Pages that are still open keep running
// the previous build's JS, and after a deploy the server no longer has its
// chunks; those requests must still resolve from the retained cache.
async function retainPreviousGeneration() {
  const names = (await caches.keys()).filter(name => name.startsWith(STATIC_PREFIX));
  const known = await readGenerations();
  for (const name of names) {
    const build = name.slice(STATIC_PREFIX.length);
    if (!known.includes(build)) known.push(build);
  }
  const ordered = known.filter(build => build !== BUILD);
  ordered.push(BUILD);
  // Cache names keep their creation order, so the tail is the newest generation.
  const keep = ordered.slice(-2);
  const keepNames = new Set(keep.map(build => STATIC_PREFIX + build));
  keepNames.add(CACHE);
  for (const name of names) {
    if (!keepNames.has(name)) await caches.delete(name);
  }
  await writeGenerations(keep);
}

async function readGenerations() {
  const cache = await caches.open(GENERATIONS);
  const response = await cache.match("/generations");
  return response ? await response.json() : [];
}

async function writeGenerations(builds) {
  const cache = await caches.open(GENERATIONS);
  await cache.put("/generations", new Response(JSON.stringify(builds), { headers: { "Content-Type": "application/json" } }));
}

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
  // Any retained generation may hold this chunk: content-hashed URLs are unique,
  // and an open page from the previous build still needs its files.
  try {
    const hit = await caches.match(request);
    if (hit) return hit;
  } catch {
    // Cache Storage unavailable; the network is still a valid answer.
  }
  const response = await fetch(request);
  if (!response.ok) return response;
  try {
    const cache = await caches.open(CACHE);
    await cache.put(request, response.clone());
  } catch {
    // Quota exhaustion or a denied Cache Storage must not turn a delivered
    // chunk into a failed request; caching is best effort.
  }
  return response;
}
