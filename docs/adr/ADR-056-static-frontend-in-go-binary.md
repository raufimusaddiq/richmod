# ADR-056: Serve the static frontend from the Go API

## Status

Accepted — 2026-11-03. Amends the deployment model of ADR-002 and ADR-019; the
frontend stack itself is unchanged.

## Context

The web app is a Next.js App Router client that loads every page with
client-side `fetch` against the Go API. It had no server actions, route
handlers, middleware, dynamic segments, `next/headers`, cookies, or
`generateStaticParams`; the only server features were `force-dynamic` and
per-route `Cache-Control` headers in `next.config.mjs`. Running `next start` in a
second container therefore added a Node runtime and an extra hop without
executing any application logic.

## Decision

1. `apps/web` builds with `output: "export"` into `out/`. The two
   `force-dynamic` exports are removed, and `app/opengraph-image.js` is marked
   `force-static` so it renders at build time. `useSearchParams()` consumers are
   wrapped in the Suspense boundary the static export requires.
2. `apps/api/internal/webui` embeds the export with `//go:embed all:assets` and
   serves it from the API binary. Route mapping: exact file, then `<path>.html`,
   then `<path>/index.html`; unknown paths return the exported `404.html` with
   status 404; `/path/` redirects 308 to `/path`.
3. Cache classes move from `next.config.mjs` headers to the Go handler:
   the seven former `no-store, max-age=0` routes keep that value, other HTML
   keeps Next's dynamic default (`private, no-cache, no-store, max-age=0,
   must-revalidate`), `/_next/static/` stays immutable for one year, and public
   files keep `public, max-age=0`. Since ADR-057's update notice, the generated
   `/build-id.txt` uses the revalidating metadata class
   (`public, max-age=0, must-revalidate`), because its value changes on every
   release.
4. API-owned paths (`/api`, `/webhooks`, `/finance`, `/healthz`, `/readyz`) keep
   the existing handlers and middleware. The UI is served outside
   `securityHeaders`, whose `Content-Security-Policy: default-src 'none'` would
   block its scripts, matching the previous behaviour where Caddy served HTML and
   the API only answered API paths.
5. `infra/docker/api.Dockerfile` builds the web export in a Node stage and copies
   it into the Go build; `infra/docker/web.Dockerfile` and the `web` Compose
   service are deleted. Caddy has one upstream, `finance-api:8080`.
6. `apps/web` keeps `npm run build:embed` for local binaries that must contain the
   UI. A tree without a web build still compiles and tests because the embedded
   directory contains only its tracked placeholder.

## Consequences

- One runtime container serves the UI and the API; no Node process in production.
- Any future server-only Next feature (route handlers, server actions, headers,
  middleware) is unavailable until the export boundary is revisited.
- Deployment is only correct when the API image is rebuilt; the Dockerfile and
  release workflow make that mandatory, and there is no separately versioned web
  image. `infra/deploy/release.sh` no longer pulls or starts `web`.
- The host-managed Caddyfile must be updated to the single-upstream site block and
  reloaded; the repository copy documents the required configuration.
- [ADR-057](ADR-057-installable-pwa-shell.md) extends this handler with the
  manifest, committed icons and a build-stamped service worker; their cache
  classes live in the same `webui` switch.
