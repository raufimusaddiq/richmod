# State machines

Every status column in Richmod is a state machine. This page lists them, the
legal moves, and **where each rule is enforced**, so the next writer does not
have to reverse-engineer it from scattered `UPDATE ... SET status=` statements.
Rule of thumb: a transition that must never happen is enforced in the database;
a transition that depends on business context is guarded by a `WHERE status IN
(...)` in the one function that owns it, and its row count is checked.

Last audited: 2026-11 (see ADR-049 amendment, migrations 00097, 00098 and 00100).

## Enforced in the database

| Machine | Legal moves | Enforced by |
|---|---|---|
| `transaction.status` | NEEDS_REVIEW → CONFIRMED / VOIDED; CONFIRMED → VOIDED / NEEDS_REVIEW; VOIDED → NEEDS_REVIEW (reversed merge only). Any → VOIDED is refused while the row is the target of an `ACTIVE` `reconciliation_merge` (unless it is itself the source of an `ACTIVE` merge) | trigger `transaction_status_transition` (00097, merge-target guard 00100, dead `PENDING` branch dropped 00102); `transaction_status_check`; timestamp CHECKs |
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

- **Undoing a confirmation (`POST /api/v1/reviews/{reviewItemId}/reopen`, "Urungkan"):** a `RESOLVED` transaction review with `resolution_action = 'CONFIRM_REVIEW'` may move its `CONFIRMED` transaction back to `NEEDS_REVIEW` only when it is the transaction's latest review item, the type is `EXPENSE`/`INCOME`/`REFUND`, no `ACTIVE` `reconciliation_merge` references it and no `salary_event` does (`reviewdomain.ConfirmedReviewReopenable`, also what `GET /reviews/resolved` reports as `reopenable`). A category-asking review clears the chosen category; a fresh `OPEN` item is inserted, the resolved item stays as history, open cycle residuals and source lifecycle are refreshed. Anything else is voided, unmerged or corrected through its own path.
- `review_item` / `review_request`: `CANCELLED` is written (reject/duplicate), `review_item` `EXPIRED` and `CANCELLED` are not.
- `review_conversation`: `AWAITING_PURPOSE`, `AWAITING_ASSET_WEALTH`.
- `salary_pending_choice`: the whole table (no producer; the remaining resolver in `agent_specialized_mutations.go` is unreachable). Do not revive without reconciling it with the pending-choice flow.
- `telegram_pending_*` `EXPIRED`, `integration_action` `DISMISSED` / `EXPIRED`: expiry is a read-time `expires_at` filter.
- `document` `RECEIVED` after creation. `transaction.status = 'PENDING'` and `transaction.type = 'ADJUSTMENT'` were removed in migration `00102` (no producer ever wrote them).

## Frontend

Screens that load data follow ADR-019's addendum: a `useGuardedLoad` status
(`idle | loading | refreshing | error | ready`) with a request-id guard,
`useDrawerRecord` for drawers, and `useBusyMap` for per-action busy state. A
reload keeps rendered children mounted; every failed request says so and offers
a retry.

## Editing a transaction's details

`PATCH /api/v1/transactions/{id}` (session auth, household-scoped, owned by
`apps/api/internal/ledger/edit.go`) lets the household file an existing
transaction from the `/transactions` drawer ("Ubah"). Edits never change status.

- **Editable:** `categoryId` (UUID or `null`), `accountId` (UUID or `null`),
  `description` (required, max 500), `note` (max 2000, `null`/empty clears) and
  `merchantName` ("Tempat · Ubah"; trimmed, whitespace-collapsed, 1-160 chars, never
  `null`; see below).
- **Locked (400 `LOCKED_FIELD`):** amount, transaction time, type, currency,
  status, purpose, related wealth account, `merchantId` (the raw canonical ID), evidence. Correcting these
  stays void + re-create so evidence integrity is preserved. Any other field is
  400 `UNKNOWN_FIELD`; an empty body is 400 `EMPTY_EDIT`.
- **Preconditions:** `If-Match: <updatedAt>` from the last read is required
  (428 `IF_MATCH_REQUIRED` if absent). A stale value is 409 `STALE_EDIT`; the row
  is locked `FOR UPDATE`, so two concurrent edits cannot both win.
- **Status:** allowed on `CONFIRMED`; `NEEDS_REVIEW` is 409
  `REVIEW_OPEN` (same boundary as confirm/void, even without a review item);
  `VOIDED` is 409 `TRANSACTION_VOIDED`. Another household's transaction is 404.
- **Review boundary (deterministic reject, no bypass):** while the transaction
  is `NEEDS_REVIEW` or has an `OPEN`/`PENDING_SEND` review item the edit is 409 `REVIEW_OPEN`
  ("selesaikan tinjauan dulu"); the review resolution path owns those facts.
  A `TRANSFER` keeps its source account and has no category, so `categoryId` and
  `accountId` and `merchantName` on a transfer are 409 `TRANSFER_FIELD_LOCKED`; its description and
  note remain editable.
- **Validation:** category and account must belong to the household and be
  active (400 `INVALID_CATEGORY` / `INVALID_ACCOUNT`). An unchanged current
  category/account is accepted even if deactivated since; only a change TO an
  inactive one is rejected, and the web form sends only changed fields. Categories carry no
  income/expense kind in the schema, so no type filter applies beyond the
  transfer rule above.
- **Merchant rename (`merchantName`):** under the same `FOR UPDATE` + `If-Match` +
  review-boundary + voided/transfer guards, Go resolves or creates the household
  `merchant` whose normalized name matches case-insensitively and sets
  `transaction.merchant_id` to it. The raw source name (`counterparty_name`, else the
  current merchant's name, else the description) is upserted into `merchant_alias`
  (`created_from_user_confirmation = true`, an existing `default_category_id` /
  `auto_apply` is kept) pointing at that merchant, so the next transaction carrying the
  same raw name resolves to the clean household name. `counterparty_name` is never
  rewritten: `GET /api/v1/transactions/{id}` returns both `merchantName` and the raw
  `counterpartyName`. Renaming to the identical merchant name is a no-op; a case-only rename (`GRAB` → `Grab`) updates the merchant name and is audited.
- **Audit:** a change writes exactly one `audit_log` row, action
  `UPDATE_DETAILS`, whose `before_json`/`after_json` hold only the changed
  fields. A rename adds a second row, action `UPDATE_MERCHANT_ALIAS` (entity `merchant_alias`,
  entity_id = the `merchant_alias` id), recording the raw name and the alias target before/after. A no-op edit writes nothing and leaves `updated_at` untouched.
- **Derived data:** analytics read the ledger live (no materialised category
  aggregates) and the cycle basis sums confirmed transactions household-wide,
  so a detail edit has no residual side effect: it never reopens a cycle review
  or creates a `review_item`.
- **Errors** return `{"error": "...", "code": "..."}`; the web app maps `code`
  to Indonesian copy and never shows the English text. No schema change.

## Voiding a transaction

`POST /api/v1/transactions/{id}/void` (`ledger/lifecycle.go`, `transition`) is the
web "Batalkan transaksi" action in the `/transactions` drawer. No transition is
added: `CONFIRMED` → `VOIDED` was already legal. There is no direct "confirm transaction" endpoint: a transaction is confirmed by its review or created `CONFIRMED`.

- **Allowed on** `CONFIRMED`. `NEEDS_REVIEW` or any open
  (`OPEN`/`PENDING_SEND`) review item is 409 ("resolve this transaction through the
  review endpoint"); the drawer then points to Tinjauan. The target of an `ACTIVE`
  `reconciliation_merge` is 409 `MERGE_TARGET` (reverse the merge first), so a merged
  real event can never end with no live transaction. This holds for **every** void path,
  not only this endpoint: the database trigger (00100) refuses the move, and each caller
  checks first with `reviewdomain.ActiveMergeTarget` and maps `ErrMergeTargetActive` to a
  clean refusal (web void/reject/transfer IGNORE → 409; Telegram reject, transfer IGNORE,
  evidence dispute and salary IGNORE → a plain Indonesian reply or `MERGE_TARGET_ACTIVE`
  tool status). The endpoint's check runs under the row lock and the merge flow locks its
  target `FOR UPDATE`, so they cannot race. The merge's own source void is allowed (the
  insert of the merge precedes it), as is voiding a transaction that is itself the source
  of an `ACTIVE` merge (chain X → A → B protects B). Reversal: `GET
  /api/v1/transactions/{id}` returns `activeMerges` (id, role `TARGET`/`SOURCE`, the
  other transaction's id/label/amount/time) and the drawer's "Pisahkan penggabungan"
  calls `POST /api/v1/reconciliation-merges/{id}/reverse`, after which the target can be
  voided. `VOIDED` again is 409 and writes nothing. Another household's transaction is 404.
- **Writes** (one DB transaction, row locked `FOR UPDATE`): `status='VOIDED'`,
  `voided_at`, `confirmed_at=NULL`, and an `audit_log` row `VOIDED` with the before/after
  status. Nothing is deleted; evidence, source events and proposals stay linked.
- **Side effects, all in that transaction:** `salary_event` follows through
  `transaction_salary_event_status`; voiding a `CONFIRMED` transaction also re-runs
  `RefreshOpenCycleResiduals` for its cycle, so an open residual review shows the new
  basis (or closes as `NO_LONGER_APPLICABLE` when nothing is left to allocate).
  Every totals query (overview, daily, category/member/cycle analytics, wealth recap,
  savings destinations, cycle basis) counts `status='CONFIRMED'` only, so a voided row
  leaves them. Voiding does not touch a residual that was already resolved/allocated.
- **Authorization:** any member of the household may void; there is no role or source
  restriction.
- Un-voiding and Telegram void are not implemented.
