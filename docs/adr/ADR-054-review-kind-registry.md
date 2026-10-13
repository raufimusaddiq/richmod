# ADR-054: Review kind registry is the single owner of review semantics

## Status

Proposed — 2026-10-13. Refines ADR-039 (ReviewDecision contract) and ADR-046
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
