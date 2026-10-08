# Architecture decision records

An ADR records a decision that changes architecture, infrastructure, or a security boundary. `AGENTS.md` requires one before any material architecture change or new infrastructure. Product decisions live in [`../bdr/`](../bdr/) as Business Decision Records.

- **Adding one:** use the next number, **ADR-052**, and the file name `ADR-052-short-title.md` with Context, Decision and Status sections like the existing records.
- **Current versus superseded:** the Status line of each record is authoritative. Where an ADR was amended, the Status names the amending ADR; read both.
- **Duplicate number:** ADR-033 is used by two records on different topics. Both are current. References elsewhere in the docs use the full file name, so this index lists both rows rather than renaming either.

This table is generated from each file's title and Status by `scripts/generate-adr-index.mjs`; run `node scripts/generate-adr-index.mjs` after adding an ADR or changing a Status. A web unit test fails when it is out of date.

| ADR | Title | Status |
| --- | --- | --- |
| [ADR-001](ADR-001-go-backend.md) | Go backend | Accepted. |
| [ADR-002](ADR-002-javascript-nextjs-frontend.md) | JavaScript Next.js frontend | Accepted. |
| [ADR-003](ADR-003-postgresql-job-queue.md) | PostgreSQL-backed job queue | Accepted. |
| [ADR-004](ADR-004-evidence-first-canonical-ledger.md) | Evidence-first canonical ledger | Accepted. |
| [ADR-005](ADR-005-cloud-llm-gateway-boundary.md) | Cloud AI Gateway boundary | Accepted — amended by ADR-030 and ADR-038. |
| [ADR-006](ADR-006-jago-spending-only.md) | Bank Jago SPENDING_ONLY policy | Accepted. |
| [ADR-007](ADR-007-deterministic-jago-parser.md) | Deterministic Jago parser with LLM fallback | Superseded by ADR-025. |
| [ADR-008](ADR-008-telegram-review.md) | Telegram human-in-the-loop review | Superseded by ADR-031. |
| [ADR-009](ADR-009-generic-document-intake.md) | Generic finance document intake | Accepted. |
| [ADR-010](ADR-010-proposals-before-untrusted-mutation.md) | Transaction proposals before untrusted mutation | Accepted. |
| [ADR-011](ADR-011-attachment-storage.md) | Attachment storage strategy | Accepted. |
| [ADR-012](ADR-012-reconciliation-scoring.md) | Reconciliation scoring policy | Accepted. |
| [ADR-013](ADR-013-idr-only-ledger.md) | IDR-only ledger | Accepted. |
| [ADR-014](ADR-014-monthly-category-budgets.md) | Monthly category budgets | Superseded by ADR-051 (2026-10-03). Budgets were never used; the handlers and routes are removed and the `budget` table is kept for history only. |
| [ADR-015](ADR-015-aggregate-only-llm-insights.md) | Tool-first cycle commentary | Accepted. |
| [ADR-016](ADR-016-restic-encrypted-backups.md) | Restic for encrypted production backups | Accepted — 2026-08-25 |
| [ADR-017](ADR-017-telegram-member-link-invites.md) | Telegram member self-link invitations | Accepted. |
| [ADR-018](ADR-018-explicit-merchant-learning.md) | Explicit merchant category learning | Accepted. |
| [ADR-019](ADR-019-routed-web-product.md) | Routed web product and dormant budgeting | Accepted. |
| [ADR-020](ADR-020-telegram-finance-assistant.md) | Telegram finance assistant queries and inline review actions | Accepted — 2026-08-26. |
| [ADR-021](ADR-021-multi-user-dashboard-auth.md) | Multi-user dashboard authentication | Not recorded |
| [ADR-022](ADR-022-telegram-interactive-callback-lane.md) | Dedicated Telegram interactive callback lane | Superseded by ADR-026. |
| [ADR-023](ADR-023-universal-review-items.md) | Universal review items | Accepted — V3 P0. Amended by ADR-046 on 2026-09-25. |
| [ADR-024](ADR-024-native-finance-tool-calls.md) | Native finance tool-call harness | Accepted — V3 P0 conversational agent. |
| [ADR-025](ADR-025-config-driven-bank-email-ingestion.md) | Config-driven bank-email ingestion | Accepted — V4. |
| [ADR-026](ADR-026-durable-telegram-ingress-and-job-lanes.md) | Durable Telegram ingress and isolated job lanes | Accepted — availability remediation Release 1. |
| [ADR-027](ADR-027-single-protocol-bounded-llm-calls.md) | Single-protocol bounded LLM calls and redacted observability | Accepted — correctness/performance remediation Release 2. **Amended by ADR-033 for Telegram free-text conversation.** |
| [ADR-028](ADR-028-canonical-universal-review-orchestration.md) | Canonical universal review orchestration | Accepted — correctness/performance remediation Release 2. Amended by ADR-046 on 2026-09-25. |
| [ADR-029](ADR-029-s3-compatible-off-host-storage.md) | S3-compatible off-host storage | Accepted — 2026-08-30 |
| [ADR-030](ADR-030-native-only-model-tool-contract.md) | Native-only model tool contract | Accepted — 2026-09-01. **Partially superseded by ADR-033 for the Telegram conversational lane.** |
| [ADR-031](ADR-031-conversational-telegram-turn-and-review-binding.md) | Conversational Telegram turns and contextual review binding | Accepted — 2026-09-01. **Amended by ADR-033 on 2026-09-12;** evidence binding extends it in ADR-050. |
| [ADR-032](ADR-032-telegram-chat-job-lane.md) | Dedicated Telegram CHAT lane | Accepted — 2026-09-01. |
| [ADR-033](ADR-033-bounded-natural-conversational-agent.md) | Bounded natural conversational finance agent | Accepted — 2026-09-12; amended by ADR-038; evidence context extends it in ADR-050. |
| [ADR-033](ADR-033-cloudflare-email-ingress-two-deploy-migration.md) | Cloudflare email ingress and two-deploy Gmail sunset | Accepted; Deploy 2 completed in production — 5 September 2026. |
| [ADR-034](ADR-034-github-release-images-and-manual-deployment.md) | GitHub release images and manual production deployment | Accepted — 4 September 2026. |
| [ADR-035](ADR-035-one-active-household-per-user.md) | One active household per user | Accepted — 5 September 2026. |
| [ADR-036](ADR-036-wealth-savings-cycle-reconciliation.md) | Wealth, Savings, and Cycle Reconciliation | Accepted |
| [ADR-037](ADR-037-intelligent-document-interpretation.md) | Bounded intelligent document interpretation | Partially superseded by ADR-051 (2026-10-03): the `shadow` and `primary` stages, the six-tool interpretation call and `RICHMOD_DOCUMENT_INTERPRETATION` are removed. The validator-feedback, one-pass field repair described below remains in force on the classify-then-extract pipeline. The rollout notes below are historical. |
| [ADR-038](ADR-038-system-one-semantic-decision-plane.md) | System One semantic decision plane | Accepted for implementation — 2026-09-20. Amended by ADR-045 on 2026-09-24. |
| [ADR-039](ADR-039-canonical-review-decision-contract.md) | Canonical ReviewDecision contract | Accepted for implementation — 2026-09-23. Implements PRD §7 (review contract). |
| [ADR-040](ADR-040-bank-email-zero-touch.md) | Bank email zero-touch classification | Accepted for implementation — 2026-09-23. Implements PRD §9 (Stage 3). Amended by ADR-045 on 2026-09-24. |
| [ADR-041](ADR-041-receipt-auto-confirm.md) | Receipt auto-confirm for clear new receipts | Accepted for implementation — 2026-09-23. Implements PRD §10 (Stage 4). Amended by ADR-045 on 2026-09-24. |
| [ADR-042](ADR-042-screenshot-row-auto-confirm.md) | Per-row screenshot auto-confirm with one bounded ruling per image | Accepted for implementation — 2026-09-23. Implements PRD §11 (Stage 5). Amended by ADR-045 on 2026-09-24. |
| [ADR-043](ADR-043-financial-email-partial-resolution.md) | Partial financial-email entity resolution | Accepted for implementation — 2026-09-24. Implements PRD §12 (Stage 6). |
| [ADR-044](ADR-044-proposal-first-review-inbox.md) | Proposal-first Review Inbox | Accepted for implementation — 2026-09-24. Implements PRD §13-§14 (Stage 7). Amended by ADR-046 on 2026-09-25 to make proposal-first rendering channel-independent. |
| [ADR-045](ADR-045-single-intelligence-pass-routing.md) | Single-intelligence-pass routing with residual bounded rescue | Accepted architecture amendment — 2026-09-24. |
| [ADR-046](ADR-046-universal-review-interaction-projection.md) | Universal review interaction projections | Accepted architecture direction — 2026-09-25. |
| [ADR-047](ADR-047-semantic-fact-ownership.md) | Semantic Fact Ownership and Accepted-Fact Boundaries | Accepted — 2026-09-27. |
| [ADR-048](ADR-048-validation-consequence-domain-continuity.md) | Validation Consequences, Residual Fidelity, and Domain Continuity | Accepted — 2026-09-27. |
| [ADR-049](ADR-049-failed-sources-surface-as-actions.md) | Failed Sources Surface as Dismissable Actions | Accepted — 2026-10-02. |
| [ADR-050](ADR-050-conversational-evidence-understanding.md) | Conversational Evidence Understanding (CEU) | Accepted — 2026-10-03 (design merged in PR #283; owner decisions recorded below). |
| [ADR-051](ADR-051-release-retirement-of-unused-capabilities.md) | Release retirement of unused capabilities | Accepted — 2026-10-03. Supersedes ADR-014. Supersedes the `shadow` and `primary` interpretation stages of ADR-037; ADR-037's one-pass field repair stays in force. |
