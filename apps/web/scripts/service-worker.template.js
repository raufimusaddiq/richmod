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
    let claims = { windows: 0, claimed: [], uncertain: false };
    try {
      claims = await clientClaims();
    } catch {
      // Claims are advisory for retention; on failure treat it as no evidence.
    }
    try {
      await retainGenerations(claims);
    } catch {
      // Pruning is housekeeping. If Cache Storage refuses, keep serving rather
      // than failing activation.
    }
    await self.clients.claim();
  })());
});

// Claim outcomes are deliberately three-valued:
//   - a build id: the window answered with its stamped build;
//   - uncertain: a window exists but did not answer in time (a suspended or
//     frozen tab), which must not be read as "nobody is using this generation";
//   - no window at all: prune immediately, because no client can be running.
const MAX_GENERATIONS = 4;
// A generation that is not claimed is kept for a day. Deploys are hours apart
// at most, so this covers a phone that suspends the tab through a release or
// two without holding storage forever.
const GRACE_MS = 24 * 60 * 60 * 1000;

async function clientClaims() {
  const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  if (windows.length === 0) return { windows: 0, claimed: [], uncertain: false };
  const answers = await Promise.all(windows.map(client => new Promise(resolve => {
    const channel = new MessageChannel();
    const timer = setTimeout(() => resolve({ timedOut: true }), 1000);
    channel.port1.onmessage = message => {
      clearTimeout(timer);
      resolve({ build: message.data });
    };
    client.postMessage({ type: "richmod:generation" }, [channel.port2]);
  })));
  const names = await caches.keys();
  const claimed = answers
    .map(answer => answer.build)
    .filter(build => typeof build === "string" && names.includes(STATIC_PREFIX + build));
  return {
    windows: windows.length,
    claimed,
    uncertain: answers.some(answer => answer.timedOut || typeof answer.build !== "string"),
  };
}

// Keep this build and every generation a window claims. An unclaimed
// generation survives until GRACE_MS has passed since it was last claimed (or
// created), which covers silent windows; with no window open there is nothing to
// protect. MAX_GENERATIONS is the last-resort ceiling: when the cap and the
// grace period disagree, the cap wins and the oldest generation goes first,
// claimed or not, except for the current build.
async function retainGenerations({ windows, claimed }) {
  const now = Date.now();
  const names = (await caches.keys()).filter(name => name.startsWith(STATIC_PREFIX));
  const entries = await readGenerations();
  const known = new Set(entries.map(entry => entry.build));
  for (const name of names) {
    const build = name.slice(STATIC_PREFIX.length);
    if (!known.has(build)) {
      entries.push({ build, seen: now });
      known.add(build);
    }
  }
  if (!known.has(BUILD)) entries.push({ build: BUILD, seen: now });

  const claimedSet = new Set(claimed);
  claimedSet.add(BUILD);
  for (const entry of entries) {
    if (claimedSet.has(entry.build)) entry.seen = now;
  }

  const ordered = entries.filter(entry => entry.build !== BUILD);
  ordered.push(entries.find(entry => entry.build === BUILD));
  let keep = ordered.filter(entry => {
    if (claimedSet.has(entry.build)) return true;
    if (windows === 0) return false;
    return now - entry.seen <= GRACE_MS;
  });
  while (keep.length > MAX_GENERATIONS) {
    let victim = -1;
    for (let index = 0; index < keep.length; index += 1) {
      if (keep[index].build === BUILD) continue;
      if (victim < 0 || keep[index].seen < keep[victim].seen) victim = index;
    }
    if (victim < 0) break;
    keep.splice(victim, 1);
  }

  const keepBuilds = new Set(keep.map(entry => entry.build));
  const retained = ordered.filter(entry => keepBuilds.has(entry.build));
  const keepNames = new Set(retained.map(entry => STATIC_PREFIX + entry.build));
  for (const name of names) {
    if (!keepNames.has(name)) await caches.delete(name);
  }
  await writeGenerations(retained);
}

async function readGenerations() {
  const cache = await caches.open(GENERATIONS);
  const response = await cache.match("/generations");
  if (!response) return [];
  const stored = await response.json();
  if (!Array.isArray(stored)) return [];
  // Earlier releases stored plain build ids; give those a fresh grace window.
  return stored
    .map(entry => (typeof entry === "string"
      ? { build: entry, seen: Date.now() }
      : { build: entry?.build, seen: Number(entry?.seen) || Date.now() }))
    .filter(entry => typeof entry.build === "string" && entry.build);
}

async function writeGenerations(entries) {
  const cache = await caches.open(GENERATIONS);
  await cache.put("/generations", new Response(JSON.stringify(entries), { headers: { "Content-Type": "application/json" } }));
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
