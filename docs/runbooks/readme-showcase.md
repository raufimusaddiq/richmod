# README visual showcase

## Purpose

`README.md` includes five product screenshots:

- `docs/assets/landing.png`
- `docs/assets/login.png`
- `docs/assets/dashboard.png`
- `docs/assets/review-inbox.png`
- `docs/assets/analytics.png`

They are rendered by Playwright, not designed mockups. The capture script lives
at `apps/web/scripts/capture-readme.mjs` and uses fixed synthetic API fixtures.
It never connects to production, sends credentials, reads `finance.env`, or
uses a real browser session.

The logo, `docs/assets/richmod-logo.svg`, is a source-controlled SVG. It is not
part of the browser capture.

## Safety boundary

Run this only from a disposable feature worktree. Never point it at a
production URL. The capture script intercepts every `/api/v1/*` browser request
and returns its own fixture data, but using a local Richmod web server keeps
the rendered application code aligned with the branch being documented.

Fixture values deliberately use a demo household and ordinary example amounts.
Do not put real names, email addresses, transaction descriptions, account
names, raw evidence, verification URLs, or secrets in the fixture or image.

## Prerequisites

- Docker available locally.
- A current `main` checkout in a dedicated feature worktree.
- The Playwright image matching `apps/web/package.json`'s pinned version. The
  commands below use `mcr.microsoft.com/playwright:v1.63.0-noble`.

The Playwright package remains a web dev dependency. The browser comes from
the disposable Docker image; no browser installation is needed on the host.

## Capture

From the feature worktree root, start the current web branch on an unused local
port. Port `3002` avoids colliding with a normal local stack on `3000`.

```bash
docker run --rm -d --name richmod-readme-web \
  -p 127.0.0.1:3002:3000 \
  -v "$PWD/apps/web:/app" -w /app \
  mcr.microsoft.com/playwright:v1.63.0-noble \
  sh -c 'npm ci --ignore-scripts --no-audit --no-fund && npm run dev -- --hostname 0.0.0.0'
```

Wait until this succeeds:

```bash
curl -fsS http://localhost:3002 >/dev/null
```

Capture all five images. `--network host` is intentional for a Linux Docker
host: it lets the disposable browser reach the loopback-only web server.

```bash
RICHMOD_SCREENSHOT_URL=http://localhost:3002 \
docker run --rm --network host \
  -e RICHMOD_SCREENSHOT_URL \
  -v "$PWD:/src" -w /src/apps/web \
  mcr.microsoft.com/playwright:v1.63.0-noble \
  npm run capture:readme
```

The script uses a 1440×1050 desktop viewport, waits for the relevant public or
authenticated shell and web fonts, disables animation, then overwrites only the
five PNG files above. It captures the public landing and login experiences plus
the dashboard, transaction Review Inbox view, and analytics page.

For an already-running local server on port `3000`, the shorter command is:

```bash
cd apps/web
npm run capture:readme
```

That command still needs the pinned Playwright browser installed locally. The
Docker path is preferred because it is repeatable and leaves no browser cache
outside the worktree.

## Verify before committing

Inspect the generated images. Confirm that each is 1440×1050, legible, current
with the UI, and contains synthetic data only.

```bash
file docs/assets/landing.png \
  docs/assets/login.png \
  docs/assets/dashboard.png \
  docs/assets/review-inbox.png \
  docs/assets/analytics.png
```

Run the web checks from the repository-root mount. Product alignment tests read
source outside `apps/web`, so mounting only that directory is insufficient.

```bash
docker run --rm -v "$PWD:/src" -w /src/apps/web node:22.14-alpine \
  sh -c 'npm test && npm run build'

git diff --check
git diff -- README.md docs/assets apps/web/scripts/capture-readme.mjs
```

Commit only the intended README, PNG, SVG when changed, Playwright script, and
package lock changes. Do not commit `apps/web/node_modules`, `apps/web/.next`,
logs, `.env`, `finance.env`, or any generated production data.

## Disposable cleanup and reclaim

After the branch has merged and its immutable CI/release image has published,
the screenshot worktree and its local dependencies are disposable. This is safe
before production deployment approval because production pulls immutable GHCR
images and keeps runtime configuration on the production host. Follow
ADR-034 and the production deployment runbook for that release sequence.

Stop the local capture server first. `--rm` removes its container after stop.

```bash
docker stop richmod-readme-web
```

Then, from the main worktree, remove only this merged feature worktree and
local branch:

```bash
git worktree remove ../family-finance-worktrees/<feature-worktree>
git branch -d <feature-branch>
git worktree prune
```

If no other task uses the downloaded Playwright image, reclaim it explicitly:

```bash
docker ps -a --filter ancestor=mcr.microsoft.com/playwright:v1.63.0-noble
docker image rm mcr.microsoft.com/playwright:v1.63.0-noble
```

Do not use broad cleanup commands such as `docker system prune`. Do not remove
production containers, volumes, `/opt/family-finance/finance.env`, another
agent's worktree, or caches not created by this capture task.
