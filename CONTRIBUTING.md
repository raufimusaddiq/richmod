# Contributing to Richmod

Thanks for helping improve Richmod.

Richmod handles financial data, so correctness and evidence preservation matter more than clever shortcuts. Before changing behavior, read [`AGENTS.md`](AGENTS.md) and the relevant current docs under [`docs/`](docs/).

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
- Next.js + React + JavaScript frontend
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
git worktree add -b feat/short-description ../richmod-worktrees/short-description main
cd ../richmod-worktrees/short-description
```

Keep unrelated changes out of the branch. Do not rewrite historical migrations; add a new migration when schema changes are required.

## Before opening a pull request

Run the checks relevant to your change. The repository CI includes Go tests and vet, database-backed integration tests, frontend tests, the Next.js production build, Compose validation, secret scanning, and production image builds.

At minimum:

```bash
# API / worker changes
cd apps/api && go test ./...
cd ../worker && go test ./...

# Web changes
cd ../web
npm test
npm run build

# Compose changes, from repository root
docker compose config
```

Not every change needs every command, but the PR should state what was run and what was not.

## Pull requests

A useful PR explains:

- the problem being solved;
- the user-visible or architectural effect;
- financial-state or household-boundary implications, if any;
- migrations or operational steps, if any;
- tests performed;
- screenshots for material UI changes.

Keep PRs cohesive. Architecture changes should update the relevant ADR or add a new one.

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
