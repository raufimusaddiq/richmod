# ADR-034: GitHub release images and manual production deployment

## Status

Accepted — 4 September 2026.

## Decision

Richmod production images are built on GitHub-hosted runners after successful
`main` CI. API, worker, web, and migration images are published to GHCR with an
immutable `sha-<full-main-commit>` tag. Image publication never deploys.

Production deployment is a separate manual GitHub Actions workflow protected by
the `production` Environment. It accepts a verified commit reachable from
`main`, connects to the host using a dedicated SSH credential, pulls the exact
four images, runs migrations, recreates application services with `--no-build`,
and checks public health. Runtime configuration and secrets remain exclusively
in the host-managed `/opt/family-finance/finance.env`.

The registry token is the ephemeral repository token and is sent over SSH stdin
for a single login. It is not written to `finance.env`; the deploy script logs
out after image pulls. Production rollback uses another immutable main SHA.

After immutable images publish successfully, merged feature worktrees and
disposable local build/dependency caches may be reclaimed before submitting the
production deployment approval. The pending deployment consumes only the
registry-hosted images and the production host's already-managed runtime state;
reclaim must never remove production containers, volumes, or `finance.env`.

## Consequences

- production CPU and disk are no longer consumed by Go or Next.js compilation;
- CI and image publication remain separate from human-approved deployment;
- each deployment is attributable to one main commit and one image set;
- GitHub Environment protection, SSH secrets, and GHCR repository access become
  deployment prerequisites;
- local `docker compose up --build` remains available for development and
  emergency recovery, but normal production deployment uses pull-only images.
- image publication success is the reclaim gate; production approval and deploy
  do not need to wait for local worktree/cache cleanup.
