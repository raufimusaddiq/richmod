# ADR-057: Installable PWA shell served by the Go binary

## Status

Accepted — 2026-11-03. Extends ADR-056; the frontend stack is unchanged.
Amended the same day after review: the service worker is a pass-through and
keeps no cache. The abandoned caching design is recorded below so it is not
re-introduced by accident. Amended again the same day (issue #408): the
installed app starts at `/login`, and iOS launch images are shipped, because a
cold start from the home screen drew a blank white frame before the first
request.

## Context

Richmod is used from a home-screen icon on iOS Safari and Android Chrome. After
ADR-056 the static export is embedded in `apps/api/internal/webui`; there is no
Node runtime, and the handler owns the cache class of every file. Installability
needs a manifest, icons and a registered service worker, but it does not need
offline support, which was never in scope.

Several review rounds tried to cache `_next/static` in the worker so installed
clients would load faster. Each fix created a new way to strand an open tab:
precaused chunks after a deploy, incomplete caches for unvisited chunks, and
finally generations kept for frozen tabs. The deciding constraint is that this
environment cannot prove what a suspended mobile tab does, so a design whose
correctness depends on that behaviour is not shippable. Serving a chunk through
both the worker cache and the network is the duplicate path that produced the
whole family of defects.

## Decision

1. **Assets are real files in the export.** `apps/web/public/manifest.webmanifest`
   and the committed PNGs under `apps/web/public/icons/` are copied into `out/`
   by `next build` and embedded by `//go:embed all:assets`. Nothing is generated
   at request time. `scripts/build-icons.mjs` rasterises the two committed SVG
   sources.
2. **Manifest.** `name`, `short_name`, `start_url: "/login"`, `scope: "/"`,
   `display: "standalone"`, colors, and 192/512 `any` icons plus a 512
   `maskable` icon whose mark sits inside the 80% safe circle. Icon dimensions
   and the maskable safe circle are asserted in `apps/web/tests/pwa.test.mjs`,
   which decodes the committed PNGs.

   `start_url` was `/` in the first revision. `/` is a real landing page for a
   signed-out visitor, so an installed app opened on marketing copy and needed
   a second tap to reach the form. `/login` starts a signed-out user on the
   form; `app/login/page.js` redirects a signed-in user to `/` once the session
   is known. `scope` stays `/` so both are inside the installed window.
3. **iOS metadata** lives in `app/layout.js`: `manifest`, `appleWebApp`
   (`capable`, `title`, `statusBarStyle: "default"`, `startupImage`), the 180px
   `apple-touch-icon`, and `themeColor` in the viewport export. Next 16 emits
   only `mobile-web-app-capable` for `appleWebApp.capable`, so the legacy
   `apple-mobile-web-app-capable` meta is rendered explicitly and React 19
   hoists it into `<head>`. `viewport-fit=cover` and the existing
   `safe-area-inset-*` rules keep the standalone window clear of the notch and
   home indicator; the drawers use the same insets.

   `appleWebApp.startupImage` is an array of `{ url, media }`; Next renders one
   `<link rel="apple-touch-startup-image">` per entry. iOS paints that image
   before it makes any request, so it is the only thing that can cover the
   cold-start gap that the pass-through worker deliberately leaves. The files
   are committed under `apps/web/public/splash/`, one per distinct portrait
   iPhone geometry, and `scripts/splash-devices.mjs` is the single source for
   both the generator (`scripts/build-splash.mjs`) and the metadata. Each media
   query carries the logical width, logical height and `-webkit-device-pixel-ratio`,
   because iOS ignores an entry that omits any of them. Tablets and landscape
   are out of scope; the launch image is the mark on the manifest background.
4. **The service worker is a pass-through.** `apps/web/public/sw.js` is a
   static public file with no cache and no request interception:

   - `install`: `skipWaiting()`;
   - `activate`: deletes the abandoned `richmod-static-*` and
     `richmod-generations` stores left by earlier releases, then
     `clients.claim()`;
   - `push`: shows a notification, falling back to a generic title and body
     when the payload is empty or malformed, because iOS drops a subscription
     after repeated pushes that show nothing;
   - `notificationclick`: focuses an open window and navigates it to the
     payload's target, or opens one when none is open; a target that is not a
     same-origin path becomes `/inbox`;
   - no `fetch` listener at all, so `respondWith` is never called and every
     request goes to the network exactly as it would without a worker.

   Push listeners do not cache: no `caches.open` call exists, and the worker
   still intercepts no request.

   HTML, RSC payloads, `_next/static`, the manifest and the API are therefore
   always fresh, and no release can leave an installed client on a build whose
   chunks the server deleted. There is no offline mode.
5. **Registration** is a production-only client component in the root layout:
   `register("/sw.js", { scope: "/", updateViaCache: "none" })`. The worker
   script is fetched on the normal update schedule; because it holds no state,
   an update cannot change what a page loads.
6. **The push card is a view of three facts, not of the last action.** The
   settings card in `apps/web/app/settings/page.js` derives everything from:
   **B**, whether this browser holds an endpoint (`true`/`false`); **S**, what
   the server says about that endpoint for this session's user and household
   (`live`/`retired`/`absent`/`unknown`); and **E**, whether push is enabled on
   the server (`enabled`/`disabled`/`unknown`). Unknown is not false. Four
   rules follow:

   1. The opt-out is offered whenever B is true. Nothing else may remove it;
      it survives an unreachable server, a disabled server, a stale row and a
      different account. It is a safety property.
   2. An endpoint is discarded only on a definitive answer about it: a
      subscription created in this call and a definitive 4xx. A 5xx, 429,
      timeout, thrown request, or a 4xx on an endpoint that already existed is
      unknown, so the endpoint is kept and the next refresh asks the server.
   3. “Aktif” is painted only when B is true and S is `live`.
   4. `E == disabled` stops new registrations. It does not mean the device is
      unsubscribed, and it never removes the opt-out.

   When a different account signs in on a device that already holds an
   endpoint, the server answers `409` with
   `code: ENDPOINT_OWNED_BY_ANOTHER_USER`; ownership is never transferred. The
   client removes the old browser subscription, subscribes a new endpoint, and
   posts that one, so the new account registers without repointing the old
   account's row. The old endpoint is reported for logging, never its keys.

   Reconciliation asks for the one endpoint the browser holds:
   `GET /api/v1/push/subscriptions?endpoint=<url>`, scoped by the same user and
   household as the sender. It returns that row (live or retired) or nothing;
   an endpoint owned by another user is indistinguishable from absent. The
   unfiltered list is paginated (`limit`, default 100, max 200; `offset`) with
   `nextOffset`, so it is never silently truncated. There is no cache in the
   worker and no second counter for the badge; `InboxCountProvider` owns the
   count.
7. **Handler classes stay as ADR-056 set them.** `webui.go` serves
   `/manifest.webmanifest` as `application/manifest+json` with
   `public, max-age=0, must-revalidate`, and `/sw.js` with `no-cache`. Icons keep
   the public class, app HTML keeps `no-store`, other HTML keeps the private
   dynamic default, `_next/static` keeps the one-year immutable browser class,
   and 404s keep `no-store`. `/build-id.txt` (see 8) uses the metadata class,
   `public, max-age=0, must-revalidate`, because that answer changes every
   release. These are HTTP cache rules between the browser and the Go server;
   the service worker does not participate.

8. **Update discovery is a build-id comparison (#413).** Because the worker
   `skipWaiting()`s and keeps no cache, `registration.waiting` is always null,
   so the signal cannot be a waiting worker as it is in Antares. Each build
   gets an identity from `apps/web/scripts/build-id.mjs`: a 12-character
   SHA-256 prefix over every file that feeds the export (`app/`, `public/`,
   `scripts/` — `app/layout.js` imports `scripts/splash-devices.mjs` — plus
   `next.config.mjs`, `package.json`, `package-lock.json`), sorted by path with
   the path hashed alongside the contents, run before every `build` and `dev`.
   Only generated output is excluded (`public/build-id.txt`,
   `app/lib/buildId.generated.js`), or the hash could not stabilise. The release SHA is deliberately not used: the container
   builds `apps/web` without `.git`. One computation writes both
   `app/lib/buildId.generated.js`, which the page imports so it knows its own
   id with no request, and `public/build-id.txt`, which `webui.go` serves for
   the deployed build. `BuildUpdateNotice` checks once at mount — every route
   mounts a new shell, so a timer started fresh on mount would never reach its
   first tick for an active user — then polls `/build-id.txt` every ten minutes
   only while the tab is visible, and checks once on `visibilitychange` to
   visible. The comparison is seeded with the page's own id, and a failed,
   empty or malformed response is "no update". It shows a dismissible
   in-flow bar ("Versi baru tersedia" + "Muat ulang") above the page header;
   the reload is the user's choice, and a dismissal lasts only this page
   load (module state, so it survives client-side navigation but not a
   reload). The root layout's shell renders it, so every signed-in page has it
   without each page knowing.

## Abandoned design

Earlier revisions of this ADR cached `_next/static` in build-named generations,
precached the full chunk set at install, kept generations claimed by open
windows through a `MessageChannel` protocol (`richmod-build` meta → build id),
and expired unclaimed generations after a 24-hour grace period under a
four-generation cap. The tests were removed with the code; the build-time
stamping scripts (`build-id.mjs`, `build.mjs`, `build-service-worker.mjs`) and
the `NEXT_PUBLIC_RICHMOD_BUILD` plumbing are gone.

Do not re-add chunk caching to the service worker without reading this section.
Any such design must answer all of these at once, which five review rounds
failed to do: a freshly deployed server has deleted the previous build's chunks;
an open tab may request a chunk it never fetched; a window may be suspended and
unable to answer; storage is finite; and there must be exactly one path that
serves a chunk. If offline support ever becomes a requirement, design it as an
explicit, separately versioned offline mode — not as an optimisation bolted onto
the install surface.

## Consequences

- Installing from the browser gives a standalone window with the same layout as
  the tab; deep links and cold starts resolve through the existing file lookup.
  A first install now opens `/login`: a signed-out user sees the form, and a
  signed-in user is redirected to `/` by the login page.
- Nothing can be served stale by the worker. A release is visible on the next
  network load, and an open tab is told about it within one poll interval
  instead of waiting for a manual reload.
- Existing installs free the old cache stores once when they activate this
  worker; nothing else migrates.
- Offline use is not supported; a cold start without network shows the
  browser's error. Background sync and offline editing remain out of scope;
  Web Push delivery is the only background feature.
- Icons must be regenerated with Chromium when the brand mark changes; the
  PNGs are committed artefacts, not build output.
- Launch images follow the same rule: `node apps/web/scripts/build-splash.mjs`
  after editing the mark or the device table. They add ~230 KiB of committed
  PNGs, fetched once by iOS and revalidated like other public files.
