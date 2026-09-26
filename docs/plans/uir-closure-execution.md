# Execution Plan — UIR Closure Gate

**Contract:** `docs/RICHMOD_UIR_CLOSURE_GATE_PRD.md`  
**Decision:** `docs/bdr/BDR-003-uir-closure-before-savr.md`  
**Drift guard:** `docs/UIR_CLOSURE_DRIFT_GUARD_CHECKLIST.md`  
**Baseline:** `main@fabe7368c76dff2032685e23ef037bcefe36dcef`

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
- expired Telegram projection;
- Wealth `PREPARE_SNAPSHOT`;
- transfer reconciliation Web / bound Telegram / agent mutation paths;
- financial-email partial / ignore / terminal paths;
- Web Review Inbox active read paths;
- Admin resolution-surface, Web Escape, and TARC calculations;
- producer/capability coverage test source.

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

## B. Bank source account binding

Required:

- if the reviewed bank source is unlinked, show bounded active household account
  choices in Telegram;
- server revalidates selected account;
- link using existing canonical source/account semantics;
- resume the same bank completion job/path.

Test:

- UNKNOWN_BANK_TEMPLATE + unlinked source;
- account selection;
- amount/time completion;
- canonical transaction/review resolves without Web.

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
- treat `PREPARE_SNAPSHOT` as voluntary richer-workflow navigation if it still
  routes to Web;
- telemetry must not classify that voluntary choice as mandatory Web escape;
- review must not be falsely marked complete.

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

A delivered dead-end fixture must not count as actionable.

Prefer existing audit/request/capability data. No new telemetry table unless
strictly required.

Exit:

Admin Review Ops can be used as a reliable rollout/SAVR baseline.

---

# UIRC-05 — structural producer/capability gate

Replace the "manual list proves exhaustive coverage" assumption.

Required property:

```text
new current review producer
        ↓
must declare/use supported ReviewDecision + Telegram completion capability
        ↓
otherwise CI fails
```

Use one small production-used source of truth.

Compatibility-only schema types remain explicitly tagged.

Do not add plugin infrastructure or database capability tables.

Tests:

- test-only unregistered producer/capability fixture fails;
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
- structural producer gate green;
- no new deterministic callback model calls;
- no SAVR-owned behavior changed.

Perform a short deployed smoke check using real current flows:

- Telegram category/date review;
- cycle residual actionability;
- bank review prerequisite/completion;
- Web ↔ Telegram stale resolution;
- Admin Review metrics sanity.

Then update UIR docs from "delivered" to "product-closed".

## Stop condition

After UIRC-06 passes:

> stop UIR work and start SAVR-00.

Do not continue cleanup merely because nearby code can be made prettier.
