# ADR-054: Review kind registry is the single owner of review semantics

## Status

Accepted — 2026-10-13. Phase 1 (the kind table) implemented the same day; later phases below are open. Refines ADR-039 (ReviewDecision contract) and ADR-046
(universal review interaction projection). Does not change BDR-002.

## Context

BDR-002 and ADR-046 decided the Review Inbox is a channel-independent backlog
resolved by one shared domain service, with Web and Telegram as projections. The
stored `review_item.decision` is meant to be the single source of truth for what a
review asks (ADR-044). The code has not finished moving there. A read of the
current tree shows the semantics of one review kind are owned in many places:

| Concern | Where it is defined today |
| --- | --- |
| Reason set and creation-time contract | `worker/reviewdec/presets.go` (`activeReasons`, `Preset`) |
| Allowed actions for the Inbox | `api/review/canonical.go` `canonicalActions` |
| Policy-dependent action rewrite on read | `api/review/handler.go` (`MISSING_PAY_DATE`) |
| Reopen/duplicate action list | `reviewdomain/reopen.go` |
| Payslip contract re-derivation | `reviewdomain/payslip.go` |
| Agent `resolve_review` action enum | `worker/telegram/tool_registry.go` `reviewActionsForType` |
| DB reason vocabulary | `review_item_review_type_check`, rewritten in 6 migrations |
| Resolution dispatch | API `Resolve` if-chain and the worker's per-type handlers |

Consequences observed in the code:

- A new reason needs a migration, a preset, a producer, an API branch, a Telegram
  renderer branch and an agent enum entry. Missing one fails silently (for example
  the `FINANCIAL_EMAIL_FACTS` comment in `reviewActionsForType`).
- `MISSING_PAY_DATE` rewrites `allowedActions`, `missingFacts`, `decisionClass` and
  `interactionMode` when the list is read, from current salary policy. The contract
  a user sees can differ from the stored contract.
- The stored actions, the agent's `resolve_review` enum and the Inbox fallback are
  three independent declarations, pinned by the golden tests added with this ADR.
  They differ by kind:
  - vocabulary: `CONFIRM_REVIEW` vs `CONFIRM`; `OWN_ACCOUNT`/`HOUSEHOLD_ACCOUNT`
    vs `OWN_ACCOUNT_TRANSFER`/`HOUSEHOLD_TRANSFER`;
  - capability: `CONFLICTING_EVIDENCE` is stored as
    `MERGE_EXISTING`/`CONFIRM_NEW_TRANSFER`/`IGNORE` but the agent is offered
    `CONFIRM`/`IGNORE`; `MISSING_TRANSACTION_DATE` is stored as
    `SET_PAY_DATE`/`IGNORE` but the agent is offered `CONFIRM`/`IGNORE`; the agent
    adds `ASSET_PURCHASE` and `RECORD_ASSET_PURCHASE` to some kinds;
  - taxonomy: the agent keys on its own kinds (`TRANSFER_RECONCILIATION`,
    `WEALTH_OBSERVATION`, `SALARY_SOURCE_CONFIRMATION`) that are not stored
    reasons, and falls back to `CONFIRM`/`IGNORE` for any unlisted kind;
  - the Inbox `canonicalActions` is only a fallback for items without stored
    actions, and returns just `IGNORE` for most transaction-bound kinds.
  Some of these differences are intended surface capabilities and some are likely
  gaps; the code does not say which. Moving them without deciding would change what
  the agent is offered.

## Decision

Introduce one Go registry, one entry per review kind, as the only place review
semantics are declared. Everything else is derived from it.

```text
ReviewKind {
  Reason           canonical reason code (the ReviewDecision reasonCode)
  Subject          which subject columns the kind may bind
  Preset           creation-time ReviewDecision (today reviewdec.Preset)
  Actions          canonical allowed actions (stored vocabulary)
  AgentActions     explicit canonical -> agent verb mapping, per kind
  Resolve          the resolver for each canonical action
}
```

1. **One declaration per kind.** `Preset`, `activeReasons`, `canonicalActions`,
   `reviewActionsForType`, the hand-copied contract in `reopen.go` and the
   Telegram capability gate are replaced by registry entries. Where two lists
   described the same thing they are collapsed. The duplicate definitions are
   deleted in the same change that switches a kind over.
2. **Agent actions are declared, not derived.** The agent keeps exactly the actions
   the golden file records today. The registry holds them as an explicit
   `AgentActions` field beside the stored `Actions`, so the differences above are
   visible in one place instead of hidden in two switches. Deciding which
   differences are gaps is a separate product decision and is out of scope here.
3. **The stored decision is not rewritten on read.** The `MISSING_PAY_DATE` policy
   variants become a creation-time property of the stored contract or an input to
   the resolver, never a mutation of the contract at list time. This is the one
   deliberate behaviour change and ships in its own PR.
4. **One dispatch.** The API and the worker call
   `registry.Resolve(kind, action)`; the if-chain in `Resolve` and the parallel
   worker dispatch are removed. `reviewdomain` already owns the mutations.
5. **The reason CHECK is generated from, or replaced by, the registry.** The
   exact mechanism is decided in the PR that touches the schema, with the
   `docs/DATABASE_SCHEMA.md` update in the same branch.
6. **Subject columns are out of scope for the first phase.** A
   `subject_type`/`subject_id` model is re-evaluated only after the registry lands.

### No shadow path, delete as you go

There is no parallel old/new execution. Each kind moves in one PR that switches its
readers to the registry and deletes the old definition. Safety comes from golden
tests, not from keeping the old code beside the new.

### Golden contract tests come first

Before any kind moves, a test pins what the agent, Telegram and Inbox can observe,
for every active reason:

- the `ReviewDecision` JSON produced by `Preset` for every active reason
  (`reviewdec/testdata/preset_contracts.golden.json`);
- `canonicalActions` for the Inbox
  (`api/internal/review/testdata/canonical_actions.golden.json`);
- the `resolve_review` action enum per agent kind
  (`worker/internal/telegram/testdata/agent_review_actions.golden.json`).

Rendered Telegram card text is already covered by `review_render_contract_test.go`.

The registry change must keep these byte-identical, except where a PR names the
deliberate change (the `MISSING_PAY_DATE` fix).

### Legacy rows

Compatibility code that exists only for historical rows (NULL `decision`,
payslip reviews with no document binding, decisions naming no missing dimension,
transaction-era `review_request` columns) is deleted once production holds no
active row in that state. A read-only count is taken first. If rows remain, they
are resolved by a migration (status change with audit, never a hard delete) and
the code is deleted afterwards.

## Implementation status

Phase 1 — done: `apps/reviewdomain/reviewdec` (moved from `apps/worker/internal`,
so the API can read it too) now holds one table, `kinds.go`, with a row per kind.
`Preset`, `ActiveReasons`, `InboxActions` (was `canonicalActions`),
`AgentActions` (was `reviewActionsForType`) and `AllAgentActions` (was
`reviewActions`) are derived from it. The hand-copied POSSIBLE_DUPLICATE contract
in `reopen.go`, its drift test, and the third variant built inline in
`screenshot_amount.go` are replaced by `Preset`. The screenshot variant stored an
invalid `decisionClass` (`CANONICAL_AMBIGUITY` is a consequence name) and
`CONFLICT_RESOLUTION`; new rows now store the shared `DUPLICATE_AMBIGUITY` /
`BOUNDED_CHOICE`. Telegram renders both modes identically and the RHICE queries
only test for `EVIDENCE_GAP`, so this has no user-visible effect.

Producers were reviewed and are intentionally left alone. `document/screenshot_choice`
and `document/payslip` already start from `Preset`. `bankemail` and `financialemail`
build their own decisions because the evidence-specific facts differ per source
(missing entities, source event, policy version, decision source), and some action
lists legitimately differ from the table: `partialDecision` stores
`COMPLETE_BANK_FACTS` for `DOCUMENT_EXTRACTION_LOW_CONFIDENCE`, where the table says
`REPROCESS_DOCUMENT`; the bank-email transfer review stores only `IGNORE`. Folding
these into `Preset` would change stored data without removing real duplication, so
the table owns the defaults and a producer owns what its evidence changes. The only
producer divergence that is a defect is the `MISSING_PAY_DATE` policy variant, which
is written at creation by `configurePayslipReviewDecision` and then rewritten again
at read time by the API.

Phase 2 — done: the payslip policy rules (`MISSING_PAY_DATE` first-salary choice,
dropping the dead `PRIMARY_SALARY` action) were written twice, once in the worker at
creation and once in the API at read time, with different `whyNotAutoConfirm` copy.
They now live in `reviewdec.PayslipPolicy`, used by both. The read-time application
is kept on purpose: the stored decision is a snapshot of salary policy, and the Inbox
must reflect the household's current policy. The Inbox copy for the first-salary case
now matches the Telegram card.

Re-assessed and deliberately not done:

- **Single `Resolve` dispatch.** The API `Resolve` handler is the Web adapter that
  ADR-046 prescribes; the shared mutations (`ResolveFinancialEmailReview`,
  `ReconcileTransfer`, `ApplyCycleResidual`, the payslip and missing-amount
  resolvers, ...) already live in `reviewdomain`. The worker's per-type handlers are
  interactive Telegram state machines, not a second dispatch of the same command.
- **Legacy-row compatibility code.** A read-only production count on 2026-10-13
  found 0 active review items of 190, and 120 resolved items with a NULL `decision`.
  No active row needs the compatibility paths, but resolved history stays NULL and can
  still be reopened through Unmerge (`ReopenTransactionReview`), and the admin and
  RHICE queries read it, so that code is not dead. The one fail-open guard
  (a payslip review with no document binding, `nullableUUID`) is a handful of lines
  threaded through finalization; removing it is not worth the churn.
- **Reason CHECK constraint.** Replacing it with a lookup table still needs a
  migration per new kind and adds a second list that can drift from the Go table.

Open: nothing is required. Remaining differences between the stored actions, the
Inbox fallback and the agent enum (see Context) are product decisions, not
duplication.

## Consequences

- Adding a review kind becomes one registry entry plus its resolver.
- The stored decision again equals what users and the agent see.
- Agent-visible behaviour is protected by tests, not by code duplication.
- No new infrastructure, service, table or dependency.
- Cost: a one-time golden-test build and a series of small, individually
  reviewable PRs. Kinds move one at a time, so the registry is partially
  populated while the work is in progress; the golden tests are the guard for that
  window and every PR must leave the tree with no duplicate definition for the
  kinds it moved.

## Alternatives considered

- **Patch each duplicate in place.** Rejected: this is how the duplication grew.
- **Run old and new paths in parallel and compare.** Rejected: it leaves two
  implementations to maintain, and the golden tests give the same safety.
- **Replace `review_item` with per-subject tables first.** Rejected as a first step:
  it is a large migration, and the registry removes most of the cost that motivates it.
