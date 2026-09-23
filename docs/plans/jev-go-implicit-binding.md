# Plan: seamless Jev↔Go binding (implicit workflow lanes)

Status: proposed — supersedes the ad-hoc fall-through added in PR #115.
Related: ADR-038 (amendment 2026-09), ADR-033.

## Problem

Go narrows the Telegram tool catalog from a server-bound workflow target before
Jev is asked anything about the current message
(`applyAgentWorkflowToolPolicy` runs before `tryJudgmentBoundWorkflow`). For
*exact* bindings this is right. For *implicit* bindings — an open review, an
awaiting merchant-learning confirmation — it lets chat state own a turn the user
intended as a new event.

Observed twice in production on 2026-09-23:

- 09:00 a merchant follow-up ("ibu kantin") was dropped;
- 10:33 a new expense ("jajan gorengan 5k") died with 0 model phases and was
  never recorded, because a single open review had narrowed the catalog to
  `resolve_review` only and the handler returned "handled".

PR #115 fixed the review case. It did not remove the class.

## Current state (after PR #115)

| Binding | Class | Behaviour on decided "not this workflow" |
|---|---|---|
| pending action | exact | hard stop (correct) |
| pending batch | exact | hard stop (correct) |
| salary choice | exact | hard stop (correct) |
| explicit review reply | exact | hard stop (correct) |
| implicit review | implicit | **falls through** (fixed in #115) |
| merchant learning | implicit | **still swallows** (latent) |

## Stage 1 — close the latent merchant-learning swallow (small)

`judgmentNoul` returns `(remember, decided, err)`; it has no decided-\"no\"
signal, so it cannot distinguish "user said no" from "not an answer". Change the
merchant-learning branch to a bounded choice, matching the review branch:

```
criteria := {REMEMBER, SKIP, OTHER_OR_UNCLEAR}
classifier error / undecided -> clarification hard stop
choice == OTHER_OR_UNCLEAR and scope == MERCHANT_LEARNING -> release binding,
    restore GeneralTools, return false
choice == REMEMBER|SKIP -> resolveNativeMerchantLearning
```

Tests (mirror #115's file):

- implicit merchant binding + new-expense text → `handled=false`, binding
  dropped, `record_transaction` back in catalog;
- undecided/error → hard stop, binding retained;
- explicit reply + decided SKIP → resolves, no new transaction.

Exit criteria: no implicit binding can swallow a message for which the
classifier returned a decided non-review answer.

## Stage 2 — route-first lane selection (ADR-level)

Replace "narrow from chat state, then ask Jev inside the lane" with:

1. run `judgmentTaskRoute` first (vocabulary already includes
   `REVIEW_INTERACTION`, `MERCHANT_LEARNING_INTERACTION`,
   `SALARY_INTERACTION`);
2. narrow the tool surface only when the route names the bound workflow;
3. keep exact reply / exact chat-scoped bindings as a deterministic override
   that outranks the route, since those are unambiguous by construction.

This removes the need for per-binding fall-through special cases: an implicit
binding is never consulted unless the route says the turn is that interaction.

Touches: `applyAgentWorkflowToolPolicy` precedence, `judgment_fast_path` ordering,
`tryJudgmentBoundWorkflow` gating, and the tests that pin today's precedence
(`agent_workflow_policy_test.go`, `agent_hardening_integration_test.go`,
`bound_review_integration_test.go`). Because precedence is part of the accepted
contract, this is an ADR amendment, not a hotfix.

Exit criteria:

- deleting the #115 fall-through branch leaves all tests green, proving route
  selection alone is sufficient;
- a chat with many open reviews routes non-review messages to general handling
  without consulting a review binding.

## Stage 3 — regression guard

Add one end-to-end Telegram test asserting the invariant directly: given an open
review and an awaiting merchant confirmation, a plain expense line records
exactly one transaction and resolves no workflow. This is the sentence that
should have existed the first time.

## Non-goals

- No change to Jev prompts, criteria, or thresholds; the defect has always been
  Go's handling of a correct bounded answer.
- No new workflow bindings without a matching fall-through test (ADR-038).
- No auto-merge or deploy from this plan; each stage ships as its own green PR.
