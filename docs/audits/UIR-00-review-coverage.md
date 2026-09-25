# UIR-00 Review Coverage Audit

Baseline: PR #159 integration branch at `16cf59e`; schema through migration
`00064`. `review_item.review_type` is called `n` in legacy Telegram projection
SQL; current API/schema use `review_type`.

`review_item` is canonical. Web lists non-transaction subjects in
`apps/api/internal/review/canonical.go`. Canonical Web mutations are in
`apps/api/internal/review/canonical.go`, but wealth observation and cycle residual
paths also mutate in `apps/api/internal/wealth/handler.go`. Telegram transaction
projection creation remains `EnqueueReviewRequest` in
`apps/worker/internal/telegram/review.go`; bound transaction handling joins the
transaction there. Separate special handlers cover transfer, cycle, and wealth
reviews. `review_request.transaction_id` is already nullable since migration
`00046`; `review_item` also permits wealth and cycle subjects since `00047`.
Therefore UIR-02 is a producer/dispatcher/renderer change, not a nullability
migration by default.

| Review type | Producer evidence | Subject | Web action | Telegram current | Telegram target / shared operation |
|---|---|---|---|---|---|
| `UNKNOWN_MERCHANT` | bank-email policy, Telegram intake | source / transaction | generic canonical review | transaction projection; purpose/category flows | FULL; resolve facts/category/ignore |
| `UNKNOWN_PURPOSE` | bank-email policy | source | canonical review | legacy purpose/detail path | FULL; complete source facts |
| `AMBIGUOUS_CATEGORY` | bank email, Telegram, receipts | source / transaction | canonical review | transaction category path | FULL; set category |
| `POSSIBLE_DUPLICATE` | receipt/screenshot reconciliation, Telegram | source / transaction | canonical duplicate action | explicitly redirects to Web | FULL; merge or confirm new |
| `CONFLICTING_EVIDENCE` | review pipeline; no active producer located in Go source | source | generic canonical review | no type-specific path found | Confirm producer status in follow-up; if produced, FULL conflict resolution |
| `UNKNOWN_EMAIL_TEMPLATE` | no active Go producer located | — | generic canonical review | none located | legacy/unverified; prove no external/SQL producer, else FULL |
| `RECEIPT_MISMATCH` | no active Go producer located | — | generic canonical review | none located | legacy/unverified; prove no producer, else FULL |
| `DOCUMENT_EXTRACTION_LOW_CONFIDENCE` | document receipt/payslip, bank email | document / source | generic canonical; document-specific inspection | specialized limited tools; no universal projection found | FULL; confirm/correct/reject extraction |
| `TRANSFER_CLASSIFICATION` | screenshot, financial email, Telegram transfer | source event | canonical transfer resolution | specialized and agent paths; ad hoc projection in places | FULL; classify/merge/confirm through shared resolver |
| `MANUAL_CORRECTION` | payslip transaction creation | transaction | canonical transaction review | ordinary transaction projection | FULL; correct residual facts |
| `DOCUMENT_CLASSIFICATION` | document processor | document | generic canonical listing; verify action coverage | no projection producer found | FULL; classify/reject document |
| `PAYSLIP_CONFIRMATION` | payslip processor | document | canonical action in review resolver | specialized agent registry, no observed general projection | FULL; salary classification |
| `MISSING_PAY_DATE` | payslip processor | document | canonical set-pay-date action | specialized agent registry, no observed general projection | FULL; set date |
| `SALARY_SOURCE_CONFIRMATION` | no active producer located in searched Go sources | — | canonical resolver branch | tool registry supports it | legacy/unverified; prove producer state, else FULL |
| `UNKNOWN_BANK_TEMPLATE` | bank-email processor | source event | generic canonical listing | tool registry supports low confidence; no projection dispatch found | FULL; complete source facts/ignore |
| `INVOICE_PAYMENT_STATUS` | no active producer located in searched Go sources | — | generic canonical listing | no projection path found | legacy/unverified; prove producer state, else FULL |
| `CYCLE_RESIDUAL_ALLOCATION` | cycle review job/domain | cycle residual case | canonical action | specialized agent and exact-bound review handlers | FULL; allocate/retain/resolve via shared resolver |
| `WEALTH_OBSERVATION_CONFIRMATION` | document and financial-email processors | wealth observation | canonical listing; separate wealth handler mutation | specialized agent handler; no universal dispatch | FULL; resolve account/confirm snapshot |
| `FINANCIAL_EMAIL_RESOLUTION` | financial-email processor | financial-email observation | canonical entity-resolution action | no general projection found | FULL; resolve household entities |
| `MISSING_TRANSACTION_DATE` | receipt/screenshot residual classification | source / transaction | canonical resolver | residual-only in newer flows; projection coverage inconsistent | FULL; set date |
| `TRANSACTION_FACTS_MISSING` | receipt/screenshot residual classification | source / transaction | canonical resolver | residual-only in newer flows; projection coverage inconsistent | FULL; provide remaining facts |

## Resolution scope

- Shared starting point: `apps/api/internal/review/canonical.go`; move channel
  decisions into a channel-neutral Go service without weakening its household,
  lock, active-review, allowed-action, candidate, audit, and RHICE checks.
- Additional direct canonical mutations to consolidate: wealth observation and
  cycle residual in `apps/api/internal/wealth/handler.go`; Telegram specialized
  paths in `apps/worker/internal/telegram/agent_review_mutations.go`,
  `agent_specialized_mutations.go`, and bound review handlers.
- Web's transaction review handlers also need enumeration at UIR-01 start; the
  canonical file is not the only Web mutation surface.
- Existing migrations explicitly support transaction-less `review_request` and
  review items for wealth/cycle. Avoid schema change until runtime audit proves a
  missing invariant. Preserve existing exact recipient/message bindings.

## Exit gaps / next tasks

1. Confirm all 21 constrained types against producers outside the Go source
   (SQL functions, jobs, fixtures); current source search found active producers
   for 15 types and did not find producers for six marked unverified above.
2. Add exhaustive producer-to-capability coverage, based on a maintained
   registry/test, during UIR-02; distinguish retired types explicitly.
3. UIR-01 first inventories every Web and Telegram mutation call site, then
   extracts shared operations incrementally with parity tests.
4. UIR-02 dispatches existing canonical items idempotently to eligible Telegram
   recipients and removes transaction-only assumptions from all bound paths.
