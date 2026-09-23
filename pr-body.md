## Problem

Go narrowed the Telegram tool catalog from chat state *before* Jev saw the message. For an **implicit** binding (an open review, an awaiting merchant confirmation) that let stale chat state own a turn the user meant as a new event — the production defect where an expense died with zero model phases and was never recorded.

PR #115 and #116 each patched one binding with a fall-through special case. The class remained, and every new implicit binding would need its own.

## Fix — route-first lane selection

Run the bounded route decision first, and narrow the catalog only when the route names the bound workflow. The route — not chat state — decides whether the turn is a workflow reply, so an implicit binding can no longer mis-own a turn.

| binding | class | behaviour |
|---|---|---|
| pending action / batch / salary choice | exact | narrows unconditionally, outranks the route |
| explicit Telegram reply | exact | narrows unconditionally, outranks the route |
| implicit review | implicit | narrows only on `REVIEW_INTERACTION` |
| merchant learning | implicit | narrows only on `MERCHANT_LEARNING_INTERACTION` |
| undecided route (empty) | — | no implicit binding narrows; fails closed on the general path |

Changes:

- thread the decided route into `applyAgentWorkflowToolPolicy`
- attach the implicit binding to `agentState` only under its matching scope, so `tryJudgmentBoundWorkflow` never sees stale chat state
- delete both per-binding fall-through branches
- delete the now-unread `WorkflowScope` and `GeneralTools` fields

## Tests

New `agent_route_first_lane_test.go`:

- `TestRouteFirstKeepsGeneralCatalogForNewTransaction`
- `TestRouteFirstMerchantBindingFollowsRoute`
- `TestExactReplyBindingOutranksRoute`
- `TestEmptyRouteDoesNotNarrowImplicitBinding`

Stage 3 regression guard in `agent_review_chat_new_expense_integration_test.go`:

- `TestOpenReviewChatRecordsNewExpenseWithGeneralCatalog`

The two fall-through test files from #115/#116 are removed: route-first makes their scenario unreachable, and the new coverage pins the stronger invariant.

## Docs

ADR-038 amendment updated to record route-first as implemented; `docs/plans/jev-go-implicit-binding.md` marks Stages 1-3 shipped.

## Verification

- `go build ./...` and `go vet ./internal/telegram/` clean
- `go test ./...` in `apps/worker` green (13/13 packages)

## Not in scope

No change to Jev prompts, criteria, or thresholds — the defect was always Go's handling of a correct bounded answer.
