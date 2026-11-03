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
//     entry can never be the wrong bytes for its URL. The whole set is
//     precached at install; the previous generation is retained on activate.
//   - everything else (manifest, icons, fonts, brand art): network-only.
// There is no precached shell. Offline browsing is deliberately out of scope.
const BUILD = "__BUILD_ID__";
const ASSETS = __ASSETS__;
const CACHE = `richmod-static-${BUILD}`;
const STATIC_PREFIX = "richmod-static-";
const GENERATIONS = "richmod-generations";

self.addEventListener("install", event => {
  event.waitUntil((async () => {
    // Cache this build's complete chunk set before taking over. Caching only the
    // chunks a page happened to load leaves holes that no later deploy can fill:
    // the old URLs are gone from the server while the page is still open.
    await precache();
    // Take over on the next launch instead of waiting for every tab to close: the
    // cache name is build-scoped and HTML is never served from cache, so an
    // immediate activation cannot hand out a stale shell.
    await self.skipWaiting();
  })());
});

async function precache() {
  try {
    const cache = await caches.open(CACHE);
    // One failing chunk must not abandon the rest; caching is best effort.
    await Promise.allSettled(ASSETS.map(async url => {
      if (await cache.match(url)) return;
      const response = await fetch(url);
      if (response.ok) await cache.put(url, response);
    }));
  } catch {
    // Cache Storage refused. Serve from the network as before; install must not
    // fail because storage is unavailable.
  }
}

self.addEventListener("activate", event => {
  event.waitUntil((async () => {
    let live = [];
    try {
      live = await claimedGenerations();
    } catch {
      // An unresponsive client falls back to the previous-generation rule.
    }
    try {
      await retainGenerations(live);
    } catch {
      // Pruning is housekeeping. If Cache Storage refuses, keep serving rather
      // than failing activation.
    }
    await self.clients.claim();
  })());
});

// Ask every open window which build it is running, then resolve that answer to a
// cache generation. A tab can stay open across several redeploys, so its
// generation must outlive the current and previous ones.
const MAX_GENERATIONS = 4;

async function claimedGenerations() {
  const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  const claims = await Promise.all(windows.map(client => new Promise(resolve => {
    const channel = new MessageChannel();
    const timer = setTimeout(() => resolve(null), 1000);
    channel.port1.onmessage = message => {
      clearTimeout(timer);
      resolve(message.data);
    };
    client.postMessage({ type: "richmod:generation" }, [channel.port2]);
  })));
  const names = await caches.keys();
  const builds = [];
  for (const chunks of claims) {
    if (!Array.isArray(chunks) || chunks.length === 0) continue;
    // The page sends every static script it runs. The generation that contains
    // the most of them is the build that page belongs to.
    let best = null;
    let bestCount = 0;
    for (const name of names) {
      if (!name.startsWith(STATIC_PREFIX)) continue;
      let count = 0;
      for (const chunk of chunks) {
        if (await caches.match(chunk, { cacheName: name })) count += 1;
      }
      if (count > bestCount) {
        bestCount = count;
        best = name.slice(STATIC_PREFIX.length);
      }
    }
    if (best) builds.push(best);
  }
  return builds;
}

// Keep this build, every generation an open window still claims, and the one
// before it for windows that did not answer. MAX_GENERATIONS caps storage; past
// it the oldest unclaimed generation goes first, then the oldest claimed one.
async function retainGenerations(live) {
  const names = (await caches.keys()).filter(name => name.startsWith(STATIC_PREFIX));
  const known = await readGenerations();
  for (const name of names) {
    const build = name.slice(STATIC_PREFIX.length);
    if (!known.includes(build)) known.push(build);
  }
  const ordered = known.filter(build => build !== BUILD);
  ordered.push(BUILD);
  // Cache names keep their creation order, so the tail is the newest generation.
  const keep = new Set([BUILD, ...live.filter(build => names.includes(STATIC_PREFIX + build))]);
  const previous = ordered[ordered.length - 2];
  if (previous) keep.add(previous);
  const claimed = new Set(live);
  while (keep.size > MAX_GENERATIONS) {
    const candidates = ordered.filter(build => keep.has(build) && build !== BUILD);
    const victim = candidates.find(build => !claimed.has(build)) || candidates[0];
    if (!victim) break;
    keep.delete(victim);
  }
  const retained = ordered.filter(build => keep.has(build));
  const keepNames = new Set(retained.map(build => STATIC_PREFIX + build));
  for (const name of names) {
    if (!keepNames.has(name)) await caches.delete(name);
  }
  await writeGenerations(retained);
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
