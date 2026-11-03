# ADR-057: Installable PWA shell served by the Go binary

## Status

Accepted — 2026-11-03. Extends ADR-056; the frontend stack is unchanged.

## Context

Richmod is used from a home-screen icon on iOS Safari and Android Chrome. After
ADR-056 there is no Node runtime in production: `apps/api/internal/webui` serves
the static export from the Go binary and owns the `Cache-Control` class of every
file. A PWA that serves a cached HTML shell is actively harmful here — the shell
names content-hashed chunks, and a release deletes them, so a stale shell is a
white page that the user cannot clear by reloading.

## Decision

1. **Assets are real files in the export.** `apps/web/public/manifest.webmanifest`
   and the committed PNGs under `apps/web/public/icons/` are copied into `out/`
   by `next build` and embedded by `//go:embed all:assets`. Nothing is generated
   at request time. `scripts/build-icons.mjs` rasterises the two committed SVG
   sources with the Playwright Chromium that is already a dev dependency.
2. **Manifest.** `name`, `short_name`, `start_url: "/"`, `scope: "/"`,
   `display: "standalone"`, colors, and 192/512 `any` icons plus a 512
   `maskable` icon whose mark sits inside the 80% safe circle. Icon dimensions
   and the maskable safe circle are asserted in `apps/web/tests/pwa.test.mjs`,
   which decodes the committed PNGs.
3. **iOS metadata** lives in `app/layout.js`: `manifest`, `appleWebApp`
   (`capable`, `title`, `statusBarStyle: "default"`), the 180px
   `apple-touch-icon`, and `themeColor` in the viewport export. Next 16 emits
   only `mobile-web-app-capable` for `appleWebApp.capable`, so the legacy
   `apple-mobile-web-app-capable` meta is rendered explicitly and React 19
   hoists it into `<head>`. `viewport-fit=cover` was already set; the shell
   already used `safe-area-inset-*` for the body, bottom nav, “more” sheet and
   main padding. The drawers gained the same insets so the detail/admin panel
   does not run under the home indicator.
4. **Service worker** (`scripts/service-worker.template.js`) is stamped by
   `scripts/build-service-worker.mjs` with a hash of the sorted `_next/static`
   file list and written to `out/sw.js` as part of `npm run build`.

   | Request class | Strategy | Why it cannot strand a client |
   | --- | --- | --- |
   | Navigations and `.txt` RSC payloads | network-only, not intercepted | HTML never comes from a cache, so it always names the current release's chunks |
   | `/api`, `/webhooks`, `/finance`, `/healthz`, `/readyz` | not intercepted | the worker never sees financial state |
   | `/_next/static/*` | cache-first across retained generations | Next hashes these names; a cache hit is byte-identical to the URL. A failing cache lookup or write falls through to the network response instead of failing the chunk |
   | manifest, icons, fonts, brand art | network-only | revalidated as before; nothing stale is served from a worker cache |

   There is no precached shell and no offline fallback page. At install the
   worker precaches the complete `/_next/static` set for its build — a page only
   fetches the chunks it uses, so caching on demand would leave holes that no
   later deploy can fill — then calls `skipWaiting()`. The release id is derived
   from the frontend sources before Next runs and stamped into every page as
   `<meta name="richmod-build">` and into the worker as `BUILD`; both come from
   `scripts/build-id.mjs`, so they are identical by construction. On activate
   the worker asks every open window for its generation through a
   `MessageChannel`; the page answers with that meta value, which maps
   one-to-one onto `richmod-static-<build>`. It retains the current build, every
   generation an open window still claims, and the immediately previous
   generation for windows that do not answer, deleting the rest. A hard cap of
   four generations keeps storage bounded; past the cap the oldest unclaimed
   generation goes first, then the oldest claimed one. `richmod-generations`
   records the ordered list. A page that was open across several redeploys keeps
   requesting its build's chunks, including chunks it never fetched before, and
   every worker answers them from the retained cache because `caches.match`
   searches all generations. Because HTML is network-only, immediate activation
   cannot expose an old shell. Cache Storage failures (denied access, quota) are
   caught: a chunk that arrived from the network is returned even when it could
   not be cached.
5. **Registration** is a production-only client component in the root layout,
   with `updateViaCache: "none"` and an extra `registration.update()` whenever
   a long-lived standalone window becomes visible. A release is picked up on
   the next foreground/navigation: the worker script bytes change, the new
   worker activates, and the new HTML is fetched from the network.
6. **Handler classes.** `webui.go` serves `/manifest.webmanifest` as
   `application/manifest+json` with `public, max-age=0, must-revalidate`, and
   `/sw.js` with `no-cache` (revalidate every time). Icons stay in the existing
   public class. Per-route HTML, `.txt` sibling and immutable-chunk rules are
   untouched; `apps/api/internal/webui/webui_test.go` covers the new files.

## Consequences

- Installing from the browser gives a standalone window with the same layout as
  the tab; deep links and cold starts resolve through the existing file lookup.
- A first visit downloads the export's whole `_next/static` set (about 1.5 MB)
  at install time instead of only the chunks that page uses. That is the cost of
  guaranteeing an open build can still resolve chunks it never touched after
  the next deploy. The precache is best effort: a failed request, a refused
  Cache Storage or browser eviction leaves holes, and those chunks then 404 once
  the server has moved on.
- The guarantee starts with clients running this worker code. A window built by
  an earlier worker cannot answer the generation question; it only benefits when
  its generation is the immediately previous one, since a chunk it never fetched
  before that worker's deploy was never precached.
- Every release changes `sw.js` (the build hash covers chunk content), so
  installed clients observe an update on their next launch. There is no prompt
  UI, no forced reload, and no update banner. Storage is bounded to four
  generations: the current and previous builds are always kept, a generation
  claimed by an open window survives further releases, and at the cap the oldest
  claimed generation is dropped even if a window still uses it.
- `apps/web/tests/service-worker.test.mjs` executes the template in a VM against
  a controllable Cache Storage: a denied `open` and a rejected `put` must still
  deliver the network chunk, install must precache the complete set, a build-A
  window held open across the activation of build B and then build C must still
  resolve a chunk it never fetched, a build-B window that shares most scripts
  with A must keep B while C and D activate, and pruning must stay within four
  generations. The built-export test additionally asserts the page's
  `richmod-build` meta equals the worker's `BUILD`.
- Offline use is not supported; a cold start without network shows the
  browser's error. Push notifications, background sync and offline editing
  remain out of scope.
- Icons must be regenerated with Chromium when the brand mark changes; the
  PNGs are committed artefacts, not build output.
