# Execution Plan — UIR Closure Gate

**Contract:** `docs/RICHMOD_UIR_CLOSURE_GATE_PRD.md`  
**Decision:** `docs/bdr/BDR-003-uir-closure-before-savr.md`  
**Drift guard:** `docs/UIR_CLOSURE_DRIFT_GUARD_CHECKLIST.md`  
**Baseline:** `main@fabe7368c76dff2032685e23ef037bcefe36dcef`

**Implementation base:** merged PR #189, `main@fe55a19a97d6993db803e278d360a9a5c0930467`.
**Progress:** UIRC-02 C: confirm timestamp boundary now accepts only `*time.Time`; Telegram adapters parse supplied dates before calling the shared operation. Other UIRC gates remain open.
**UIRC-01 B partial:** bank amount/time preflight now shares the canonical positive whole-IDR limit; async pre-commit reply says processing, not recorded. Unlinked-source Telegram account binding and post-commit terminal delivery remain open.
**UIRC-01 A implementation:** cycle `TRANSACTION_MISSING` continues through normal Telegram intake; newly confirmed income/expense refreshes matching open cycle reviews in the same transaction and closes only if no positive residual remains. CI integration test required before exit.
**UIRC-01 B implementation:** unlinked bank review shows bounded active household funding accounts, stores submitted facts in its existing conversation, revalidates selected account against the open review, links the source, then queues the same `COMPLETE_BANK_REVIEW` job. A terminal post-worker notification remains open.

### UIRC-00 re-audit (merged PR #189 base)

- Cycle Web escape: `apps/worker/internal/telegram/review.go` `resolveNativeResidualReview` and `agent_review_mutations.go` `agentResolveResidual`; canonical recomputation already lives in `apps/reviewdomain/cycle.go`, while `residual/processor.go` only refreshes a positive case's basis when invoked.
- Bank unlinked/false-success: `review.go` `completeBankFactsReply` + `parseBankFactsReply`; `reviewdomain/bank_facts.go` source validation; `bankemail/processor.go` COMPLETE_BANK_REVIEW mutation.
- Expired projection: `review.go` `processBoundReview` sends Web-only expired message, despite canonical item still open.
- Wealth Web action: `api/internal/review/canonical.go` `reviewActions` and `ResolveCanonical`; `telegram/agent_bound_mutations.go` `agentResolveBoundWealthObservation` and `telegram/review.go` `resolveNativeSpecialReview`.
- Investment mapping: `reviewdomain/transfer.go` `ErrInvestmentAccountAmbiguous`; `telegram/review.go` `resolveTransferReview` and `agent_review_mutations.go` agent transfer classifier.
- Transfer reconciliation mutation: `api/internal/review/canonical.go` `resolveTransferReconciliation`; `telegram/review.go` `resolveNativeTransferCase`; `telegram/agent_review_mutations.go` agent transfer-case path.
- Financial-email lifecycle: `api/internal/review/canonical.go` `ResolveCanonical`; `telegram/review.go` financial-email reply path. Inbox reads: `api/internal/review/handler.go` transaction-first list and `canonical.go` `listCanonical`.
- Metrics: `api/internal/admin/reviews.go` ReviewOpsSummary/Breakdown/Projections; actionability currently counts delivered `telegram_message_id`, surface looks up `RESOLVE_REVIEW` audit.
- Producer coverage: `telegram/review_render_contract_test.go` manually curated `producibleReviewTypes`; production presets `telegram/reviewdec`; action lists `telegram/tool_registry.go`.
- Confirm date: Web `api/internal/review/handler.go` passes `*time.Time`; Telegram date, ordinary confirm and agent confirm were `*string`, `*string`, `time.Time` respectively; all five callers now use typed `*time.Time`.

The ordinary `allowed_actions` action-by-action capability audit remains open; this is not UIRC-00 exit evidence yet.

## Operating rule

This is a closure sprint, not UIR v2.

For every task:

1. fetch latest `main`;
2. re-check the exact gap before changing code;
3. implement the smallest durable fix;
4. do not touch SAVR-owned semantics;
5. add the narrow regression test;
6. run the drift guard;
7. one bundled push per review cycle;
8. wait for review of the exact latest PR head before another push.

---

# UIRC-00 — baseline proof

Re-audit latest main and record the exact current call sites for:

- cycle `TRANSACTION_MISSING`;
- bank review with unlinked source account;
- bank-fact parser/validator parity and pre-commit success wording;
- expired Telegram projection;
- Wealth `PREPARE_SNAPSHOT`;
- transfer `INVESTMENT_ACCOUNT` ambiguity / Wealth candidate continuation;
- transfer reconciliation Web / bound Telegram / agent mutation paths;
- financial-email partial / ignore / terminal paths;
- Web Review Inbox active read paths;
- Admin resolution-surface, Web Escape, and TARC calculations;
- producer/capability coverage test source;
- every active ReviewDecision ordinary `allowed_action` and its actual Telegram
  terminal/continuation path;
- every `ConfirmTransactionReview` caller and current `TransactionAt` runtime type.

Exit:

No implementation begins from stale assumptions.

---

# UIRC-01 — remove mandatory Web escapes

## A. Cycle residual: TRANSACTION_MISSING

Required:

- keep the cycle review canonical/open while the missing transaction is added;
- route the user through the existing Telegram canonical transaction intake;
- after successful transaction commit, trigger the existing cycle residual
  refresh/reconciliation;
- do not add a new cycle-specific transaction form.

Test:

- start from cycle review;
- choose missing transaction;
- record canonical transaction through Telegram;
- refreshed residual review is closed or updated;
- no `requires_web=true` / "open Review Inbox" ordinary path.

## B. Bank source binding + bank-fact completion truth

Required:

- if the reviewed bank source is unlinked, show bounded active household account
  choices in Telegram;
- server revalidates selected account;
- link using existing canonical source/account semantics;
- resume the same bank completion job/path;
- validate the Telegram amount/time against the same canonical bank invariants
  before queueing the job;
- reject signed/non-positive or otherwise invalid facts without queueing;
- do not answer "recorded" before canonical persistence has succeeded. If the
  current architecture acknowledges before the worker runs, use non-terminal
  processing wording and emit terminal success only from/after the canonical job.

Test:

- UNKNOWN_BANK_TEMPLATE + unlinked source;
- account selection;
- amount/time completion;
- canonical transaction/review resolves without Web;
- `-54000` queues no completion job and yields no false-success reply;
- valid asynchronous completion cannot claim canonical commit before persistence.

## C. Expired projection continuation

Required:

- if projection expired but canonical item is open, renew/re-project actionability;
- no canonical review state is discarded;
- do not force Web.

Test:

- expired request + open item;
- interaction results in fresh actionable projection;
- only one canonical item remains.

## D. Wealth snapshot action classification

Required:

- do not implement full Telegram snapshot authoring;
- ensure `SET_WEALTH_ACCOUNT` / `IGNORE` remain Telegram-completable;
- if `PREPARE_SNAPSHOT` still routes to Web, remove it from the canonical
  `ReviewDecision.allowed_actions` completion set and render it only as optional
  secondary navigation;
- if it remains an ordinary allowed action, it must instead have a Telegram-native
  continuation/completion lane;
- telemetry must not classify true voluntary navigation as mandatory Web escape;
- review must not be falsely marked complete.

Test:

- the Wealth review's ordinary allowed-action matrix contains no Web-only action;
- optional snapshot navigation leaves the canonical review open/actionable and is
  excluded from Web Escape Rate.

## E. Investment transfer ambiguity

Required:

- when investment classification cannot deterministically map to one Wealth
  Account, present active compatible household Wealth Account candidates in
  Telegram;
- selected candidate is server-ID bound and revalidated under the review lock;
- continue through the same shared transfer classifier;
- do not require Settings / Review Inbox and do not add a Telegram settings editor.

Test:

- zero/multiple deterministic mapping produces bounded candidates;
- selected candidate completes canonical transfer classification;
- stale/foreign/inactive candidates fail closed;
- no mandatory Web redirect remains.

Exit:

No active ordinary review blocker requires Web.

---

# UIRC-02 — finish shared canonical resolution boundary

## A. Transfer reconciliation

Move the duplicated reconciliation mutation into one narrowly scoped
`apps/reviewdomain` operation.

The shared operation owns:

- reconciliation-case lock/state;
- candidate validation;
- transaction confirm/create;
- evidence attachment;
- source/financial-observation refresh;
- reconciliation-case resolution;
- canonical review/projection completion.

Adapters own:

- HTTP / callback / agent decoding;
- candidate display/pagination;
- user-facing text;
- surface-specific audit metadata.

Tests:

- same fixture through Web and Telegram;
- equivalent canonical result;
- stale/cross-household candidate rejected;
- first valid resolution wins.

## B. Financial-email lifecycle

Consolidate only duplicated state transitions:

- partial entity binding;
- ignore;
- final entity binding;
- source transition + replay enqueue;
- canonical item/request completion.

Do not create a generic workflow engine.

Tests:

- Web and Telegram partial two-step entity resolution;
- ignore parity;
- concurrent stale action parity.

## C. Typed transaction-confirm date boundary

Narrow `reviewdomain.ConfirmCommand.TransactionAt` from `any` to one explicit
optional timestamp representation (preferred `*time.Time`). Surface adapters own
parsing/household-timezone normalization before entering the domain operation.

Required:

- no typed-nil interface state can cross the shared confirm boundary;
- category-only/no-date confirmation preserves the stored proposal timestamp;
- existing explicit date updates remain equivalent;
- do not change natural-language date semantics (SAVR-owned).

Tests:

- absent date cannot overwrite proposal/transaction timestamp;
- explicit Web/Telegram/agent dates reach the same canonical type/result;
- regression equivalent to PR #188 stays green without type-switching over
  multiple pointer representations.

Exit:

No covered review family has separate Web-vs-Telegram canonical mutation rules.

---

# UIRC-03 — make review_item the active Inbox authority

Required:

- prove every current active review producer creates `review_item`;
- render current active Inbox work from canonical item + ReviewDecision;
- if historical transaction-only compatibility is still necessary, isolate it as
  explicit legacy fallback;
- add a test that current producers do not require the legacy fallback.

Do not migrate historical rows unless a correctness defect requires it.

Exit:

`review_item` is not merely a product claim; it is the authority for current
review work.

---

# UIRC-04 — fix Admin review metric semantics

## Resolution surface

Use actual resolution action/surface from canonical audit information.

Do not infer surface from Telegram account ownership.

Test:

- Telegram-linked user resolves from Web -> WEB;
- same user resolves from Telegram -> TELEGRAM;
- system closure -> SYSTEM.

## Web Escape Rate

Count only mandatory surface switches from a Telegram review path.

Exclude voluntary:

- View details;
- richer Wealth snapshot navigation;
- independent user choice to use Web.

Test each case.

## TARC

Numerator requires both delivery and current Telegram completion capability.

A delivered dead-end fixture must not count as actionable. Actionability is
computed from the current decision's ordinary allowed-action set: every such
action must have a Telegram terminal or Telegram continuation capability.

Prefer existing audit/request/capability data. No new telemetry table unless
strictly required.

Exit:

Admin Review Ops can be used as a reliable rollout/SAVR baseline.

---

# UIRC-05 — structural producer/capability gate

Replace the "manual list proves exhaustive coverage" assumption.

Required property:

```text
new current review producer OR new ordinary allowed action
        ↓
must declare/use supported ReviewDecision + Telegram terminal/continuation capability
        ↓
otherwise CI fails
```

Use one small production-used source of truth.

Compatibility-only schema types remain explicitly tagged.

Do not add plugin infrastructure or database capability tables.

Tests:

- test-only unregistered producer/capability fixture fails;
- a registered type with one unregistered/dead-end ordinary `allowed_action`
  fails independently of the type-level gate;
- compatibility-only values remain render-safe but are not counted as active
  producer coverage.

Exit:

The repository cannot silently add a current Web-only review type.

---

# UIRC-06 — final product closure gate

Run the regression matrix from the closure PRD plus existing UIR tests.

Required final evidence:

- backend/frontend/container/security checks green;
- UIR contract tests green;
- no active mandatory Web escape;
- shared resolution parity tests green;
- actual-surface Admin metrics green;
- structural producer + allowed-action gate green;
- bank invalid-fact / false-success regression green;
- investment-transfer ambiguity remains Telegram-native;
- shared confirm timestamp boundary is explicitly typed;
- no new deterministic callback model calls;
- no SAVR-owned behavior changed.

Perform a short deployed smoke check using real current flows:

- Telegram category/date review;
- cycle residual actionability;
- bank review prerequisite/completion, including invalid fact rejection;
- investment transfer with ambiguous Wealth mapping;
- Wealth optional snapshot navigation classification;
- Web ↔ Telegram stale resolution;
- Admin Review metrics sanity.

Then update UIR docs from "delivered" to "product-closed".

## Stop condition

After UIRC-06 passes:

> stop UIR work and start SAVR-00.

Do not continue cleanup merely because nearby code can be made prettier.
