# Contributing to Richmod

Thanks for helping improve Richmod.

Richmod handles financial data, so correctness and evidence preservation matter more than clever shortcuts. Before changing behavior, read [`AGENTS.md`](AGENTS.md) and any relevant decision-log entry or runbook under [`docs/`](docs/).

## Development principles

- PostgreSQL is canonical financial state.
- Go owns financial state transitions.
- LLM output is untrusted input and must pass deterministic validation before persistence.
- Financial mutations must remain household-scoped and auditable.
- Source evidence is preserved; deduplication links evidence instead of deleting it.
- Ambiguity goes to Review Inbox rather than being guessed.
- Webhooks and jobs must be idempotent.
- Deterministic features must continue to work when the LLM gateway is unavailable.

## Stack

- Go backend and workers
- React + JavaScript frontend built with Bun
- PostgreSQL
- PostgreSQL-backed jobs
- Cloud LLM Gateway
- Docker Compose

Do not introduce a new runtime, queue, database, model host, or infrastructure dependency without an explicit architecture decision.

## Workflow

Repository file changes use a dedicated branch and linked worktree.

```bash
git switch main
git pull
git worktree add -b feat/short-description ../family-finance-worktrees/short-description main
cd ../family-finance-worktrees/short-description
```

Keep unrelated changes out of the branch. Do not rewrite historical migrations; add a new migration when schema changes are required.

`main` is protected. Push the branch, open a pull request, and wait for the required checks (CI and the automated Hermes Review) to pass; the pull request is then merged with a merge commit. Never push to `main` directly. The full sequence, including release images and deployment approval, is the [sprint delivery runbook](docs/runbooks/sprint-delivery.md).

## Before opening a pull request

Run the checks relevant to your change. The repository CI includes Go tests and vet, database-backed integration tests, frontend tests, the Bun production build, Compose validation, secret scanning, and production image builds.

At minimum:

```bash
# API / worker changes
cd apps/api && go test ./...
cd ../worker && go test ./...

# Web changes
cd ../web
bun install --frozen-lockfile --ignore-scripts
bun run test
bun run build

# Any change, from repository root: comments are banned
node scripts/check-comments.mjs

# Compose changes, from repository root
docker compose config
```

Not every change needs every command, but the PR should state what was run and what was not. [`docs/runbooks/disposable-test-matrix.md`](docs/runbooks/disposable-test-matrix.md) has the exact commands for isolated database, web and browser runs.

Interface changes also run the browser tests: `cd apps/web && bun run test:e2e` (builds, then runs `playwright test`; see [ADR-119](docs/adr/README.md)). The screenshot baselines are Linux-only, so `playwright test` skips the `visual` project on Windows and macOS; `bun run test:visual` runs it inside the pinned Playwright image (`apps/web/scripts/visual-docker.sh`), because a host Chromium rasterises the same self-hosted fonts differently and every baseline then fails. When a change is meant to alter the screenshots, regenerate the baselines with `bun run test:visual:update`, look at the images, and commit only the ones that should change. Assert rendered behavior in `apps/web/e2e`, not application source text. Keep interface text at 12px or larger, label every control, and give any scrolling region a name and keyboard focus; the smoke and `apps/web/tests/ui-audit-locks.test.mjs` enforce the text floor and keyboard-reachable scrolling regions. Check the page at phone, tablet and desktop widths. [`docs/brand-guidelines.md`](docs/brand-guidelines.md) has the tokens.

## Pull requests

A useful PR explains:

- the problem being solved;
- the user-visible or architectural effect;
- financial-state or household-boundary implications, if any;
- migrations or operational steps, if any;
- tests performed;
- screenshots for material UI changes.

Keep PRs cohesive. A material architecture change should add a short entry to the [decision log](docs/adr/README.md).

## Financial correctness checklist

For changes that can affect financial state, verify that:

- household identity is selected deterministically;
- the LLM cannot directly mutate state;
- monetary values do not use floating point;
- duplicate delivery or job execution is safe;
- ambiguous input fails to review instead of guessing;
- existing evidence and audit history remain intact.

## Security issues

Do not report vulnerabilities in a public issue. Follow [`SECURITY.md`](SECURITY.md).
