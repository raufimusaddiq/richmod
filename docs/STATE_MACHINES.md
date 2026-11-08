# State machines

Every status column in Richmod is a state machine. This page lists them, the
legal moves, and **where each rule is enforced**, so the next writer does not
have to reverse-engineer it from scattered `UPDATE ... SET status=` statements.
Rule of thumb: a transition that must never happen is enforced in the database;
a transition that depends on business context is guarded by a `WHERE status IN
(...)` in the one function that owns it, and its row count is checked.

Last audited: 2026-11 (see ADR-049 amendment, migrations 00097 and 00098).

## Enforced in the database

| Machine | Legal moves | Enforced by |
|---|---|---|
| `transaction.status` | PENDING → CONFIRMED / NEEDS_REVIEW / VOIDED; NEEDS_REVIEW → CONFIRMED / VOIDED; CONFIRMED → VOIDED / NEEDS_REVIEW; VOIDED → NEEDS_REVIEW (reversed merge only) | trigger `transaction_status_transition` (00097); timestamp CHECKs |
| `salary_event.status` | follows its transaction (VOIDED ⇄ CONFIRMED) | trigger `transaction_salary_event_status` (00097) |
| `review_item` → `review_request` → `review_conversation` | item RESOLVED closes its request; request RESOLVED/CANCELLED resolves its conversations (not EXPIRED, not `AWAITING_MERCHANT_DECISION`) | triggers `review_item_close_request`, `review_request_close_conversation` (00098) |
| `review_request` card delivery | see `review_request_recipient` below | triggers/functions of 00079 and 00094 |
| `review_item.decision` | an item may never be (re)opened without a valid decision | trigger `review_item_require_decision` (00078) |
| `job.lane` | assigned from the type | trigger `enforce_job_lane` (00081) |

## Enforced in Go (single owner)

| Machine | States | Owner | Notes |
|---|---|---|---|
| `job.status` | PENDING → RUNNING → SUCCEEDED / FAILED; RUNNING → PENDING (retry, defer) | `apps/worker/internal/queue` | A lease has an owner: `Succeed`, `Defer` and the failure write require `locked_by`; running jobs renew the lease; a reclaim past `max_attempts` fails the job. Every job type that settles a row has a final-failure step (`terminalStep`, ADR-049 amendment). |
| `source_event.processing_status` | RECEIVED → PROCESSING → PROCESSED / NEEDS_REVIEW / IGNORED / FAILED | `reviewdomain.RefreshSourceLifecycle` derives it after every review transition; processors write the first outcome | `RefreshSourceLifecycle` keeps a FAILED source FAILED unless it gained accepted evidence. FAILED sources surface as Tindakan items (ADR-049). |
| `document.status` | RECEIVED → CLASSIFIED → EXTRACTED / NEEDS_REVIEW / FAILED | document processors; settled by `RefreshSourceLifecycle` | A downstream job (payslip, receipt, screenshot) that exhausts its attempts fails the document and, when nothing else came of the source, the source. |
| `transaction_proposal.proposal_status` | PENDING / NEEDS_REVIEW → ACCEPTED / REJECTED / MERGED; REJECTED → NEEDS_REVIEW (reopen); MERGED → NEEDS_REVIEW (unmerge) | moves in lockstep with the guarded transaction update that caused it | Unmerge only reopens MERGED proposals of the active evidence link. |
| `financial_email_observation.status` | PENDING → APPLIED / REVIEW / IGNORED | `reviewdomain` resolvers, each behind a locked review item | |
| `financial_email_preview.status` | PENDING → PROCESSING → SUCCEEDED / FAILED | financialemail processor | stays PROCESSING while the job retries; the job's final failure marks it FAILED. |
| `insight.status` | PENDING → SUCCEEDED / FAILED | insight processor | the job's final failure marks it FAILED. |
| `dashboard_account_invite` / `telegram_link_invite` | PENDING → CONSUMED / REVOKED / EXPIRED | `auth`, `household`, `telegram` intake, all under `FOR UPDATE` | An invite for a deactivated user is refused, not consumed. Linking Telegram holds the member row (`FOR SHARE`) so a concurrent removal cannot miss the new identity. |
| `telegram_pending_card_choice.status` | PENDING → RESOLVED | worker `processCardChoiceCallback` | the tap claims the choice first; a failed finalization hands it back. |
| `telegram_pending_action`, `telegram_pending_batch` | PENDING → CONFIRMED / CANCELLED | worker, behind `FOR UPDATE ... status='PENDING' AND expires_at>now()` | A correction staged before its transaction was voided is cancelled, not applied. |
| `review_request_recipient.delivery_status` | QUEUED / PENDING → SENT / FAILED / SKIPPED / EXPIRED | `admit_review_card`, `release_review_cards`, `expire_review_card_recipient` (SQL functions) | best-encoded machine; EXPIRED → SENT revival is intentional. |
| `push_outbox_delivery.status` | PENDING → SENT / FAILED / DEAD | push sender | |
| `email_ingress_address.status` | PROVISIONED → ACTIVE → DISABLED | emailingress service, under `FOR UPDATE`; partial unique index keeps one live address | |
| `financial_email_source.status` | DRAFT → ACTIVE → DISABLED | `settings/financial_email.go`, one writer | |

## Known dead or unreachable states

These are CHECK-constraint members nothing writes today. They are harmless;
remove them only together with a migration and the code that reads them.

- `review_item` / `review_request`: `CANCELLED` is written (reject/duplicate), `review_item` `EXPIRED` and `CANCELLED` are not.
- `review_conversation`: `AWAITING_PURPOSE`, `AWAITING_ASSET_WEALTH`.
- `salary_pending_choice`: the whole table (no producer; two diverged resolvers in `telegram/processor.go` and `agent_specialized_mutations.go` are unreachable). Do not revive without reconciling them.
- `telegram_pending_*` `EXPIRED`, `integration_action` `DISMISSED` / `EXPIRED`: expiry is a read-time `expires_at` filter.
- `document` `RECEIVED` after creation, `transaction` `PENDING` (never inserted).

## Frontend

Screens that load data follow ADR-019's addendum: a `useGuardedLoad` status
(`idle | loading | refreshing | error | ready`) with a request-id guard,
`useDrawerRecord` for drawers, and `useBusyMap` for per-action busy state. A
reload keeps rendered children mounted; every failed request says so and offers
a retry.
