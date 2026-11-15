# Richmod documentation

The code is the primary documentation. This index lists what remains under
`docs/`; earlier PRDs, audits, plans, business decision records and checklists
were removed from the tree and are recoverable from git history.

## Start here

| Document | Read it for |
| --- | --- |
| [`README.md`](../README.md) | What Richmod is, how data gets in, quick start and status |
| [`AGENTS.md`](../AGENTS.md) | Architecture rules, safety invariants and the required worktree workflow |
| [`CONTRIBUTING.md`](../CONTRIBUTING.md) | How to propose and verify a change |
| [`SECURITY.md`](../SECURITY.md) | Reporting a vulnerability |

## Operating and delivering changes

| Runbook | Read it for |
| --- | --- |
| [`runbooks/sprint-delivery.md`](runbooks/sprint-delivery.md) | The full sequence from branch to merge, release images, deployment approval and cleanup |
| [`runbooks/production-deployment.md`](runbooks/production-deployment.md) | Deploying released images, migrations, rollback, and health checks |
| [`runbooks/disposable-test-matrix.md`](runbooks/disposable-test-matrix.md) | Isolated PostgreSQL, Go, web, Compose and Playwright runs, and how to reclaim them |
| [`runbooks/stuck-source-events.md`](runbooks/stuck-source-events.md) | Detecting source events shown as needing review that have no reachable review item (#356) |
| [`runbooks/cloudflare-email-ingress.md`](runbooks/cloudflare-email-ingress.md) | The email transport: Workers, R2, Queue, HMAC contract, forwarding setup |
| [`runbooks/cloudflare-email-cli.md`](runbooks/cloudflare-email-cli.md) | The same Cloudflare setup and its live checks as `cf` and Wrangler commands |
| [`runbooks/readme-showcase.md`](runbooks/readme-showcase.md) | Regenerating the README screenshots from synthetic data |

## Architecture and data

- [`adr/`](adr/README.md): the architecture decision log, one short entry per decision. Add an entry for a material architecture or infrastructure change.
- [`DATABASE_SCHEMA.md`](DATABASE_SCHEMA.md): schema reference and ERD. Schema changes update it in the same branch.
- [`STATE_MACHINES.md`](STATE_MACHINES.md): every status machine, its legal moves, and where each rule is enforced.

## Interface

- [`brand-guidelines.md`](brand-guidelines.md): colors, type scale, hit areas and shape, enforced by `apps/web/tests/ui-audit-locks.test.mjs`.
- [`UI_MOTION.md`](UI_MOTION.md): the motion scale, what animates and why, the single reduced-motion guard, and what is deliberately still.
- [`assets/`](assets/): README screenshots and brand logos, used by `README.md` and regenerated per the readme-showcase runbook.
