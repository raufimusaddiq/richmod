# Richmod documentation

`docs/` holds about 150 Markdown files. Most are feature-specific PRDs, delivery
checklists and Codex briefs written while a feature was built. This page lists
the files to start from. When two documents disagree, follow the priority order
in [`AGENTS.md`](../AGENTS.md#project-docs): the latest explicit instruction,
then current repository behavior, then current docs and ADRs, then older specs.

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
| [`runbooks/cloudflare-email-ingress.md`](runbooks/cloudflare-email-ingress.md) | The email transport: Workers, R2, Queue, HMAC contract, forwarding setup |
| [`runbooks/readme-showcase.md`](runbooks/readme-showcase.md) | Regenerating the README screenshots from synthetic data |

## Architecture and data

- [`adr/`](adr/README.md): architecture decision records, with an index. Add one before changing architecture or infrastructure.
- [`bdr/`](bdr/): business decision records, the product decisions behind the ADRs.
- [`DATABASE_SCHEMA.md`](DATABASE_SCHEMA.md): schema reference and ERD. Schema changes update it in the same branch.
- [`RICHMOD_JEV_SYSTEM_ONE_PRD.md`](RICHMOD_JEV_SYSTEM_ONE_PRD.md): the System One (Jev) semantic decision plane.

## Product

- [`RICHMOD_PRODUCT_ALIGNMENT_V2.md`](RICHMOD_PRODUCT_ALIGNMENT_V2.md): the product alignment the current surfaces follow.
- [`MVP_COMPLETION_CHECKLIST.md`](MVP_COMPLETION_CHECKLIST.md): what the MVP covers and how it was verified.
- [`ANALYTICS_CYCLE_REVIEW_UI.md`](ANALYTICS_CYCLE_REVIEW_UI.md): the current Analytics contract. Older analytics insight and chart-refinement documents are historical.

## Interface and quality

- [`brand-guidelines.md`](brand-guidelines.md): colors, type scale, hit areas and shape, enforced by `apps/web/tests/ui-audit-locks.test.mjs`.
- [`audits/UI-AUDIT-2026-10-02.md`](audits/UI-AUDIT-2026-10-02.md): the phone, tablet and desktop UI audit, what each fix changed, and what remains open.
- [`RICHMOD_UI_REDESIGN_2026-09-07.md`](RICHMOD_UI_REDESIGN_2026-09-07.md): the redesign the current interface grew from.

## Folders

| Folder | Contents |
| --- | --- |
| [`adr/`](adr/README.md) | Architecture decision records |
| [`bdr/`](bdr/) | Business decision records |
| [`runbooks/`](runbooks/) | Operating procedures |
| [`audits/`](audits/) | Point-in-time audits and their closure notes |
| [`plans/`](plans/) | Execution plans for multi-sprint work |
| [`assets/`](assets/) | README screenshots and the logo |

The remaining top-level files are feature PRDs, checklists and briefs. Check the
ADR index and the runbooks first, and treat a checklist or Codex brief as the
record of how something was built, not as the current specification, unless a
current document above links to it.
