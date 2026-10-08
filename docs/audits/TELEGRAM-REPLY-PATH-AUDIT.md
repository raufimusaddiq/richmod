# Telegram typed-reply path audit

**Baseline:** `432f1ec` (`main`, deployed 2026-10-03), with PR #296
(`fix/telegram-receipt-date-reply`, head `8e0b78f`) measured alongside.
**Status:** Audit only. No code changed. Findings are evidence-backed; each
"broken" row was reproduced by a probe through the production entry point.
**Trigger:** a production report: replying to a receipt's "Tanggal transaksi
belum ada" card never stored the date.

## 1. Question

When a household member types a reply to a review card in Telegram, does the
code path production actually runs complete the review, and does any test drive
that same path?

## 2. Production entry points (verified)

`apps/worker/cmd/worker/main.go` dispatches:

| Job | Entry | Carries |
| --- | --- | --- |
| `PROCESS_TELEGRAM_TEXT` | `Processor.ProcessAgent` | every typed message, replies included |
| `PROCESS_TELEGRAM_CALLBACK` | `Processor.Process` | button taps only |

Inside `ProcessAgent`, a typed reply to a review card goes one of two ways
(`agent.go:55-73`):

1. **Proposal lane.** If the card's review is keyed to a proposal with no
   transaction (`repliesToProposalReview`), the reply goes to
   `processBoundReview`. In practice: `MISSING_AMOUNT` and payslip
   `MISSING_PAY_DATE` / `PAYSLIP_CONFIRMATION`.
2. **Agent lane.** Everything else. `exactAgentReviewBinding` binds the card
   only if its review has a transaction, a cycle-residual case, a wealth
   observation or a transfer-reconciliation case; otherwise **no binding**
   (`default: return nil, nil`). With a binding, Jev's bounded review workflow
   runs first (`tryJudgmentBoundWorkflow`), then the generative model with
   `resolve_review`.

Inside `Process`, button taps are routed by callback data. The generic
`processBoundReview` call at `processor.go:217` is reached **only by typed
text**, because every callback returns before it (`sourceType ==
"TELEGRAM_CALLBACK"` → "stale"), except the five transfer buttons, which call
`processBoundReview` earlier. Production never sends typed text to `Process`.

## 3. Method

1. Traced the dispatch above by reading the code.
2. Classified every Telegram test by entry point and input kind (script in the
   audit transcript): which tests send **typed text into `Process`**.
3. Probed: temporary tests (not committed) that create each typed-reply card the
   way producers do, send the reply through `ProcessAgent` with Jev configured
   and a model that makes the obviously correct `resolve_review` call, then read
   canonical state. Two Jev behaviours: `jev-confirm` (Jev picks `CONFIRM`, the
   likely real answer to a direct reply) and `jev-unclear` (Jev answers
   `OTHER_OR_UNCLEAR`, so the generative model runs). Run against `main` and
   against PR #296, on a disposable migrated PostgreSQL 17.

## 4. Results

### 4.1 Typed-reply matrix

"Works" means the reply's value reached canonical state and the review
advanced. Card input "typed" means the card asks the household to type a value.

| Review type (card) | Card input | Production lane | `main` jev-confirm | `main` jev-unclear | #296 jev-confirm | Test on production entry |
| --- | --- | --- | --- | --- | --- | --- |
| `MISSING_TRANSACTION_DATE` (receipt/screenshot date) | typed date | agent | **broken** | **broken** | works | #296 adds them |
| `TRANSACTION_FACTS_MISSING` (date then category) | typed date | agent | **broken** | **broken** | works (advances to category) | none |
| `UNKNOWN_PURPOSE` | typed purpose | agent | **broken** | works, then re-asks category ¹ | **broken** | none |
| `MANUAL_CORRECTION` | typed detail | agent | **broken** | works, then re-asks category ¹ | **broken** | none |
| `UNKNOWN_BANK_TEMPLATE` (bank facts) | typed amount + time | agent, **unbound** | **broken** | **broken** | **broken** | none |
| `UNKNOWN_MERCHANT` (merchant first) | typed merchant | agent | works | works | works | `merchant_first_integration_test.go` |
| `TRANSFER_CLASSIFICATION` | buttons or typed | agent / callbacks | works ² | — | — | `core_boundary_transfer_review_integration_test.go` |
| `POSSIBLE_DUPLICATE` | buttons or typed | agent / callbacks | works ² | — | — | `evidence_candidates_integration_test.go` |
| `MISSING_PAY_DATE` (payslip) | typed date | proposal | works ² | — | — | `evidence_review_continuation_integration_test.go` |
| `MISSING_AMOUNT` (screenshot row) | typed amount | proposal | not probed ³ | — | — | **none** |
| Category chooser, salary policy, document, financial-email, wealth, residual cards | buttons | callbacks | out of scope ⁴ | | | callback tests exist |

¹ The agent's save step moves to the category chooser even when the review's
decision does not list category as missing and the transaction already has one.
The old reply lane confirms in that case (`saveBoundReviewField`); the agent copy
(`agentSaveReviewField`) lacks that branch.
² Covered by an existing test that drives `ProcessAgent`; not re-probed.
³ Reachable (proposal lane), but its resolver needs a document/attachment
fixture; no Telegram test exists for a typed amount reply at all.
⁴ Button taps go through `Process`, which is the real production path for them.

### 4.2 What the household sees when it is broken

- jev-confirm, typed-value cards: "Masih ada detail review yang perlu
  dilengkapi." The model is never called (`modelCalls=0`). Jev picks `CONFIRM`,
  `CONFIRM` needs no argument, Go confirms with no value and returns
  `MISSING_REVIEW_DETAIL`.
- jev-unclear, date cards on `main`: the model proposes the date, Go reads it
  from `description` and stores nothing; the agent writes its own prose (the
  production report: "Tanggal transaksi masih belum tersimpan…").
- Bank facts: the card is shown, but the agent has no binding for a
  source-event review, so `resolve_review` is not even offered. No
  `COMPLETE_BANK_REVIEW` job is queued; the review stays open.

## 5. Findings, by severity

### F1 — Jev `CONFIRM` short-circuits every typed-value card except merchant (high)

`judgment_workflows.go` treats `CONFIRM` as needing no argument and resolves
immediately, except for `AWAITING_MERCHANT`. Date, compound, purpose and
manual-correction cards therefore fail on `main` whenever Jev answers `CONFIRM`.
PR #296 adds `AWAITING_DATE`; **`AWAITING_DETAIL` is still missing**, so purpose
and manual-correction replies remain broken after #296. The exception list is
hand-kept instead of derived from the card's missing fact.

### F2 — Bank-fact cards cannot be completed from Telegram (high)

`UNKNOWN_BANK_TEMPLATE` reviews are keyed to a source event only. They are
projected to Telegram (`TelegramCompletableReviewType` includes them) and ask for
a typed reply, but:

- the proposal lane ignores them (no `proposal_id`);
- the agent lane has no binding kind for them (`exactAgentReviewBinding` returns
  nil);
- the handler that does complete them, `completeBankFactsReply`, is only
  reachable through `Process` with typed text, which production never does.

The only test (`multi_recipient_race_integration_test.go`) drives `Process` with
typed text, so it passes while production is broken.

### F3 — Two copies of "save the review answer" have drifted (medium)

`saveBoundReviewField` (old lane) and `agentSaveReviewField` (agent lane) each
write merchant / detail / date. Observed drift:

- date: agent copy wrote it into `description` (fixed in #296 by a shared
  `applyReviewTransactionDate`);
- detail: agent copy always moves to the category chooser; old copy confirms
  when no category is missing (footnote ¹);
- the old copy has no caller in production for typed text (section 6).

### F4 — Tests exercise a path production does not run (high, systemic)

Eleven tests send typed text into `Process`. They cover exactly the flows found
broken above, and pass:

| Test | Card |
| --- | --- |
| `bound_review_integration_test.go`: `TestTelegramReplyToBoundMerchantReviewBypassesLLM`, `TestTelegramBareMerchantResolvesOnlyOpenReview` | merchant |
| `category_chooser_integration_test.go`: `TestCategoryOnlyReviewOffersCategoryChooser` | category (typed part) |
| `date_review_integration_test.go`: all three tests | date, compound |
| `duplicate_transfer_parity_integration_test.go`: `TestTransferReviewOffersChooserAndCompletes`, `TestDuplicateReviewOffersChooserAndCompletes` (typed parts) | transfer, duplicate |
| `freeform_residual_integration_test.go`: both tests | purpose, manual correction |
| `payslip_review_integration_test.go`: `TestTelegramPayslipPolicyAndDateResolveWithoutWeb` (typed part) | payslip date |
| `multi_recipient_race_integration_test.go`: both tests (via `seedTelegramReply`) | bank facts |

`deadcode` cannot see this: `processBoundReview` is reachable (proposal lane and
transfer buttons), only some of its branches are not.

### F5 — The model is not told what the card asks for (medium)

`active_review` carried only type, amount and label; `resolve_review` offers a
set of optional fields (`description`, `pay_date`, `transaction_at`, …). The
model must guess the field. #296 adds `awaiting_field` for date and merchant
cards. The durable fix is to derive the tool arguments from the review's
`missingFacts`.

### F6 — `MISSING_AMOUNT` typed replies have no Telegram test (medium)

Production-reachable through the proposal lane; nothing exercises it end to end.

## 6. Code reached only by tests

Branches of `processBoundReview` that run only when typed text enters `Process`
(production never does), verified by the callback routing in section 2:

- the implicit no-reply binding at its top (`ProcessAgent` only calls it with an
  explicit reply);
- the bank-facts branch → `completeBankFactsReply` (F2);
- the transaction-keyed tail for typed text: `AWAITING_MERCHANT` /
  `AWAITING_DETAIL` / `AWAITING_DATE` → `saveBoundReviewField`, the typed income
  choice, the category-only and duplicate typed branches.

Still live: the `MISSING_AMOUNT` and payslip-date branches (proposal lane) and the
transfer-button branch (callbacks). Each "test-only" branch must be either
deleted with its tests, or (bank facts) moved behind a path production reaches.

## 7. Correction to the release cleanup audit

`RELEASE-CLEANUP-AUDIT.md` section 11 says `processBoundReview` was kept because
"`agent.go:70` now calls it on the live agent path". That is true only for
proposal-keyed reviews and transfer buttons; its typed-text branches for
transaction-keyed and bank-fact reviews are test-only (section 6). The release
audit measured function reachability, which cannot see this.

## 8. Recommended fix sprint

In order, each with a `ProcessAgent` test that fails first:

1. **F1:** derive "this `CONFIRM` needs a typed value" from the card's
   conversation state / missing fact instead of a hand list; covers
   `AWAITING_DETAIL` (and keeps date and merchant).
2. **F2:** give bank-fact reviews an agent binding kind with a `COMPLETE_BANK_FACTS`
   executor that calls the existing shared operation (the one the Web
   `COMPLETE_BANK_REVIEW` job uses).
3. **F3:** one save operation per fact, shared by both lanes, including the
   "confirm when nothing else is missing" rule; then delete the test-only
   branches of section 6.
4. **F4:** move the eleven tests onto `ProcessAgent` (or delete those that only
   pin removed code); add a `MISSING_AMOUNT` typed-reply test (F6).
5. **F5:** generate `resolve_review` arguments from `missingFacts`.

Merging PR #296 first is safe: it fixes date and compound cards and does not
change the other rows.

## 9. Re-measurement on `ea6ffea` (2026-10-08)

Since the baseline, `main` merged #296 (date replies), #297 (Abaikan on bank and
transaction reviews), #300 (screenshot time matching) and #301 (a plain message,
not a reply, routes into the one open review when Jev chooses
`REVIEW_INTERACTION`). The probes were re-run on `ea6ffea` for both an exact reply
and a plain message, with Jev routing the plain message to the review:

| Card | Reply, Jev `CONFIRM` | Reply, Jev unclear | Plain, Jev `CONFIRM` | Plain, Jev unclear |
| --- | --- | --- | --- | --- |
| `MISSING_TRANSACTION_DATE` | works | works | works | works |
| `TRANSACTION_FACTS_MISSING` | works (→ category) | works (→ category) | works (→ category) | works (→ category) |
| `UNKNOWN_PURPOSE` | **broken** (F1) | works, re-asks category (F3) | **broken** (F1) | works, re-asks category (F3) |
| `MANUAL_CORRECTION` | **broken** (F1) | works, re-asks category (F3) | **broken** (F1) | works, re-asks category (F3) |
| `UNKNOWN_BANK_TEMPLATE` | **broken** (F2) | — | **broken** (F2) | — |

Status of the findings on `ea6ffea`:

- **F1** open for `AWAITING_DETAIL` (purpose, manual correction); closed for date
  by #296. #301 widens its reach: plain messages now enter the same workflow.
- **F2** open; #297 adds an Abaikan button path for bank reviews only.
- **F3** open (detail re-asks category).
- **F4** open: the same eleven tests (and the two bank-fact tests through
  `seedTelegramReply`) still send typed text into `Process`. #296 added
  production-path tests for date replies only.
- **F5** partly addressed (#296 `awaiting_field`; #301 passes it to the route
  request). Tool arguments are still not derived from `missingFacts`.
- **F6** open.

## 10. Execution record (branch `fix/telegram-reply-paths`)

| Finding | Status | Test (drives `ProcessAgent`) |
| --- | --- | --- |
| F1 Jev `CONFIRM` short-circuit | fixed: rule derived from `requiredNativeReviewDetail` | `TestTypedDetailReplyCompletesReview` |
| F2 bank facts | fixed: `BANK_FACTS` binding + executor sharing `enqueueBankFactsCompletion` with the bound reply lane | `TestTypedBankFactsReplyQueuesCompletion`, both `multi_recipient_race` tests moved to `ProcessAgent` |
| F3 detail re-asks category | fixed: agent save confirms when nothing else is missing | `TestTypedDetailReplyCompletesReview`, `TestTypedPurposeThenCategoryButtonCompletes` |
| F4 tests on the wrong entry | fixed: tests of live behaviour moved to `ProcessAgent`; the typed-text lane in `Process` and the tests that only drove it removed | no test feeds typed text to `Process` (classifier: 0) |
| F5 typed tool arguments | fixed: `focusReviewArguments` narrows `resolve_review` to the arguments the bound card's actions read | `TestReviewToolArgumentsFollowTheCard` |
| F6 missing amount untested | fixed (path already worked) | `TestTypedAmountReplyRecordsScreenshotRow` |

New findings while executing:

- **F7 — duplicate merge buttons never shown (fixed: the projector stores the
  candidates and renders one merge button per candidate; covered by
  `TestDuplicateReviewOffersChooserAndCompletes`).**
  A production `POSSIBLE_DUPLICATE` card is sent with only "Catat sebagai baru" /
  "Abaikan" (`duplicateIntentMarkup`). The per-candidate merge buttons and the
  stored `duplicate_candidates` come only from `offerDuplicateChoices`, which only
  the typed text lane in `Process` reaches. Merging works in production only by a
  typed answer through the agent (`MERGE_EXISTING`). Either store candidates and
  render `duplicateChoicesMarkup` at projection, or delete the button feature.
  Chosen: render at projection, since the handler and markup were already
  built and tested.
- **F8 — typed replies to cards older than 7 days were refused (fixed).** Only
  `Process` renewed an expired projection; the agent's save step required
  `expires_at > now()`. `ProcessAgent` now renews the same way. Covered by
  `TestMultiRecipientBankRaceFirstReplyWinsSecondIsStale` (it expires the card
  first; fails without the renewal).

All findings F1–F8 are closed on this branch. `processBoundReview` now serves
only the proposal lane (missing amount, payslip date) and the transfer buttons;
`deadcode` and `staticcheck` report no new unreachable code.
