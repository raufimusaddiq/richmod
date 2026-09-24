# RICHMOD — MINIMAL HUMAN INTERACTION & VALID FINANCIAL DATA

## Final Product Requirements Document for Codex

**Status:** APPROVED PRODUCT DIRECTION / IMPLEMENTATION CONTRACT  
**Repository:** raufimusaddiq/richmod  
**Baseline branch:** main  
**Audited baseline commit:** 9e3a0a18b81844df3daadc9801f063d026dda6c7  
**Baseline date:** 2026-09-23  
**Backend:** Go  
**Frontend:** Next.js + React + JavaScript  
**Database:** PostgreSQL  
**Generative inference:** Cloud LLM Gateway / LiteRouter only  
**Bounded semantic inference:** Jev / System One through LiteRouter only  
**Currency:** IDR  
**Timezone:** Asia/Jakarta  

Related architecture and implementation references:

- docs/adr/ADR-038-system-one-semantic-decision-plane.md
- docs/adr/ADR-037-intelligent-document-interpretation.md
- docs/plans/jev-go-implicit-binding.md
- current Review Inbox implementation
- current Telegram conversational agent
- current Bank Email pipeline
- current receipt and transaction-screenshot pipeline
- current Financial Provider Email pipeline

---

# 0. Normative Instructions for Codex

This document defines intended product behavior, not a loose design suggestion.

Before implementation, Codex MUST:

1. fetch current main;
2. verify that the audited baseline commit above is still an ancestor of current main;
3. read AGENTS.md;
4. read current relevant ADRs and implementation;
5. inspect current migrations and use the next unused migration number;
6. preserve unrelated behavior;
7. treat existing tests as compatibility requirements unless this PRD explicitly supersedes that behavior;
8. add tests for every changed product invariant;
9. never reduce review volume merely by lowering Jev/model thresholds;
10. never weaken deterministic financial correctness in order to improve UX metrics.

If main changed after the audited baseline, adapt file names and implementation details to current code while preserving the product contract in this document.

This PRD supersedes the old implicit behavior:

~~~text
not deterministic
→ NEEDS_REVIEW
~~~

The required behavior is:

~~~text
not deterministic
→ bounded intelligence
→ deterministic validation
→ review only residual uncertainty
~~~

---

# 1. Product Problem

Richmod already has substantial intelligence:

- deterministic Go rules;
- merchant/category memory;
- evidence extraction;
- generative LLM extraction;
- Jev / System One bounded semantic judgment;
- duplicate/reconciliation logic;
- Review Inbox;
- Telegram conversational workflows.

Several production paths still behave like AI-assisted form filling.

The system frequently:

1. extracts information correctly;
2. verifies information correctly;
3. knows most or all required financial facts;
4. still creates NEEDS_REVIEW;
5. asks the user to type or select data that Richmod already knows.

Examples:

- Bank Email with a new merchant always asking for a category;
- merchant-less bank transactions forcing a merchant value;
- clear new receipts always creating a review when they do not match an existing transaction;
- every unmatched transaction-screenshot row creating a review;
- Financial Provider Email asking for both entities when one has already been resolved;
- Review Inbox showing complete forms instead of the smallest unresolved decision;
- Telegram semantic failures asking again for amount/date/type even when only category is uncertain;
- valid Jev routes being terminated by a generic fast-path default instead of falling through to the native-tool agent.

This wastes the value produced by LLM + Jev.

The goal is NOT to maximize AI calls.

The goal is to minimize required human effort while maintaining valid, auditable canonical financial data.

---

# 2. North Star

## 2.1 North Star Principle

> The user should only provide information or judgment that Richmod cannot safely derive from source evidence, deterministic state, Jev, or generative extraction.

User attention is the scarce resource.

Models and compute are implementation resources.

Whenever the system already knows a fact with sufficient support, the user MUST NOT be required to type or select it again.

---

## 2.2 North Star Metric — RHICE

Primary product metric:

**Required Human Inputs per Canonical Financial Event (RHICE)**

Definition:

~~~text
total explicit user inputs required before an event reaches
a valid canonical state
/
number of canonical financial events
~~~

An explicit input includes:

- button/callback choice;
- dropdown choice;
- manually typed field;
- submitted correction.

Passive viewing does not count.

Target behavior:

~~~text
clear financial event:
RHICE = 0

one genuinely unresolved bounded decision:
RHICE <= 1

N genuinely unresolved facts:
RHICE <= N

known information:
must never increase RHICE
~~~

---

## 2.3 High-Friction Diagnostic Metric

Track separately:

**Typed Fields per Canonical Financial Event**

Typing is substantially more expensive than accepting or selecting a bounded option.

For ordinary transactions, desired steady state is:

~~~text
Typed Fields / Event ≈ 0
~~~

---

## 2.4 Correctness Guardrail

Review reduction MUST NOT come from unsafe guessing.

Primary guardrail:

**Post-Auto-Confirm Material Correction Rate**

Track later material corrections to automatically confirmed events, including:

- amount;
- transaction type;
- transaction date/time;
- category;
- account relationship;
- duplicate/reconciliation state.

Review reduction is successful only when RHICE falls without a material regression in correctness.

Also monitor:

- incorrect duplicate merges;
- incorrect transfer classification;
- auto-confirm reversal rate;
- unresolved evidence-conflict rate.

---

# 3. Architecture and Product Invariants

## 3.1 Financial Authority Hierarchy

The authority hierarchy remains:

~~~text
1. deterministic Go
2. Jev / System One for bounded semantic decisions
3. generative LLM for unbounded extraction, reasoning, vision, or prose
4. Go validates and performs canonical mutation
~~~

PostgreSQL remains canonical.

No AI model directly mutates financial state.

---

## 3.2 Human Interaction Is the Final Fallback

Before asking the user, Richmod MUST attempt applicable resolution in this order:

~~~text
source evidence
    ↓
deterministic exact rules / stored memory
    ↓
Jev bounded semantic decision
    ↓
generative extraction/reasoning when arbitrary values are required
    ↓
deterministic validation
    ↓
human input for remaining uncertainty only
~~~

The user is not an extraction engine.

---

## 3.3 Known Facts Must Never Be Requested Again

If Richmod already knows:

~~~text
amount = 53,000
transaction_at = 2026-09-23 13:45
direction = OUTGOING
~~~

and only category is unresolved, the UI MUST request only category.

Amount, date, direction, merchant, account, or note must not become required fields merely because a review exists.

---

## 3.4 Confirmation Is Not Data Entry

A bounded confirmation is acceptable when genuinely necessary:

~~~text
Richmod thinks: Makanan & Minuman

[Benar] [Ubah]
~~~

This is not equivalent to:

~~~text
Merchant: [________]
Amount:   [________]
Date:     [________]
Category: [dropdown]
~~~

The former validates an intelligent proposal.

The latter makes the user redo the intelligence work.

---

## 3.5 Missing Enrichment Is Not Invalid Financial Data

Merchant is useful enrichment, but MUST NOT universally be treated as a required canonical transaction field.

A valid expense may have:

~~~text
amount        = 53,000
type          = EXPENSE
transactionAt = valid supported time
category      = valid supported category
merchant      = NULL
~~~

Richmod MUST NOT require the user to invent a merchant when source evidence does not contain one.

---

## 3.6 Model Self-Confidence Is Not Canonical Authority

Generative fields such as confidence and category_confidence may remain observability signals.

They MUST NOT become the sole mutation authority on a Jev-enabled workflow.

When an output domain is bounded, semantic choice should return to Jev and then to deterministic Go validation.

---

## 3.7 Provider Failure Is Not Semantic Uncertainty

If Jev fails because of timeout, gateway failure, rate limiting, malformed response, missing answer, or invalid probability, Richmod MUST NOT silently let a generative model become the semantic authority for the same bounded decision.

Allowed outcomes include:

- deterministic resolution;
- safe retry;
- clarification;
- minimal review;
- safe non-mutation response.

---

# 4. Residual Uncertainty Model

Every financial candidate should conceptually be represented as:

~~~text
Known Facts
+
Proposed Facts
+
Missing Facts
+
Conflicting Facts
+
Human Policy Choices
~~~

Only the final three groups may create required human interaction.

A review MUST NOT exist merely because some portion of an event came from AI.

---

# 5. Review Creation Rule

Generic form-based review is prohibited when the system can identify the exact unresolved dimension.

Normative rule:

~~~text
if missing_facts is empty
and conflicting_facts is empty
and no duplicate ambiguity exists
and no explicit human-policy choice is required
then ordinary manual review SHOULD NOT be created
~~~

Legitimate exceptions include:

- first primary-salary-source classification;
- salary-cycle residual allocation;
- unknown transfer ownership/relationship when evidence cannot establish it;
- correction of already canonical historical data;
- genuinely ambiguous duplicate candidates;
- explicit destructive or consequential actions that current product policy requires the user to approve.

---

# 6. Human-Required Decision Classes

Every new review should map to one of these conceptual classes.

## 6.1 EVIDENCE_GAP

A required canonical fact is genuinely absent or undecidable.

Examples:

- transaction date genuinely absent;
- amount genuinely unreadable;
- required category remains undecided.

Ask only for the missing fact.

## 6.2 EVIDENCE_CONFLICT

Two supported interpretations disagree.

Example:

~~~text
bank email amount = 53,000
receipt amount    = 58,000
~~~

Expose the conflict. Do not silently choose one.

## 6.3 DUPLICATE_AMBIGUITY

Multiple canonical records are plausible matches.

Ask the user to choose among bounded candidates.

Do not auto-merge.

## 6.4 HUMAN_POLICY_CHOICE

No objectively correct answer is derivable from evidence.

Examples:

- first payslip: PRIMARY_SALARY vs ORDINARY_INCOME;
- salary-cycle residual allocation;
- unknown transfer relationship.

## 6.5 CORRECTION_CONFIRMATION

Changing already canonical historical state should remain deliberate where current policy requires confirmation.

---

# 7. Canonical ReviewDecision Contract

The current review pipeline loses semantic detail between intelligence and UI.

Introduce a first-class ReviewDecision contract.

Exact storage shape may follow current schema conventions, but the following semantics are mandatory:

~~~text
ReviewDecision
  version

  subject
    type
    id

  source_event_id

  reason_code
  decision_class

  known_facts
  proposed_facts
  missing_facts
  conflicting_facts

  bounded_choices

  evidence_refs

  decision_source
  decision_policy_version
  decision_provenance

  why_not_auto_confirm

  allowed_actions

  interaction_mode
~~~

---

## 7.1 known_facts

Facts already supported and not requiring user entry.

Example:

~~~json
{
  "amount_idr": "53000",
  "transaction_at": "2026-09-23T13:45:00+07:00",
  "type": "EXPENSE",
  "merchant": null
}
~~~

Known facts render as information, not required editable fields.

---

## 7.2 proposed_facts

Richmod's best currently supported interpretation.

Example:

~~~json
{
  "category_slug": "food-and-drink"
}
~~~

The UI should present the proposal before editing controls.

---

## 7.3 missing_facts

Only facts Richmod genuinely cannot safely resolve.

Example:

~~~json
[
  "category"
]
~~~

The default UI MUST request only these facts.

---

## 7.4 conflicting_facts

Use when supported evidence disagrees.

Example:

~~~json
{
  "amount_idr": [
    {
      "value": "53000",
      "evidence": "BANK_EMAIL"
    },
    {
      "value": "58000",
      "evidence": "RECEIPT"
    }
  ]
}
~~~

---

## 7.5 bounded_choices

Choices MUST come from server-owned candidate sets.

Example:

~~~json
[
  {
    "key": "food-and-drink",
    "label": "Makanan & Minuman"
  },
  {
    "key": "groceries",
    "label": "Belanja Rumah"
  }
]
~~~

Jev never chooses hidden canonical database IDs.

Go maps approved bounded values to canonical IDs.

---

## 7.6 decision_source

Suggested values:

~~~text
DETERMINISTIC
JEV
GENERATIVE_EXTRACTION
DETERMINISTIC_PLUS_JEV
GENERATIVE_PLUS_JEV
USER_POLICY
~~~

---

## 7.7 why_not_auto_confirm

This MUST be meaningful enough for product telemetry and debugging.

Bad:

~~~text
needs review
~~~

Good:

~~~text
category decision remained inside Jev undecided probability band
~~~

or:

~~~text
two existing transactions exceeded duplicate-match threshold
~~~

---

## 7.8 interaction_mode

Suggested values:

~~~text
ONE_TAP_CONFIRMATION
BOUNDED_CHOICE
SINGLE_FIELD
CONFLICT_RESOLUTION
POLICY_CHOICE
~~~

Generic FORM should be exceptional rather than the normal mode.

---

# 8. Telegram Orchestration Requirements

## 8.1 Exhaustive Jev Route Dispatch

The current server-owned route vocabulary includes routes such as:

- READ_SPENDING
- READ_CASHFLOW
- READ_SAVINGS
- READ_WEALTH
- SEARCH_TRANSACTIONS
- CREATE_TRANSACTION
- CREATE_TRANSFER
- CORRECT_TRANSACTION
- REVIEW_INTERACTION
- SALARY_INTERACTION
- MERCHANT_LEARNING_INTERACTION
- FINANCE_HELP
- NEEDS_GENERATIVE_AGENT
- OUT_OF_SCOPE
- OTHER_OR_UNCLEAR

The fast path MUST NOT terminate valid non-fast-path routes as unclear.

Required behavior:

~~~text
fast-path-owned route
→ handle and terminate

valid route requiring agent/tools
→ return control to conversational agent

OUT_OF_SCOPE
→ terminal scoped response

OTHER_OR_UNCLEAR
→ clarification
~~~

CREATE_TRANSFER, SEARCH_TRANSACTIONS, CORRECT_TRANSACTION, FINANCE_HELP, NEEDS_GENERATIVE_AGENT, and other valid non-fast-path routes must not fall into a generic "Permintaannya belum cukup jelas" response.

---

## 8.2 Exhaustive Route Regression Test

Add a table-driven test enumerating every server-owned route.

Every route must map to exactly one lane:

~~~text
FAST_PATH_TERMINAL
AGENT_FALLTHROUGH
WORKFLOW
CLARIFICATION
OUT_OF_SCOPE
~~~

Adding a future route without defining its lane must fail the test.

---

## 8.3 Exact Bindings Outrank Generic Route Classification

ADR-038 must be implemented literally.

Examples of exact bindings:

- explicit reply to a review message;
- pending correction;
- pending batch operation;
- exact salary-choice workflow where server state uniquely owns the reply.

For exact bindings:

~~~text
exact binding
→ bound workflow interpretation
~~~

Generic route classification MUST NOT be able to discard the bound turn first.

If the exact bound reply is unclear, clarify inside that workflow.

---

## 8.4 Implicit Bindings Remain Route-Gated

Examples:

- one open review inferred from chat state;
- pending merchant-learning confirmation inferred only from chat state.

For implicit bindings:

~~~text
generic route first
~~~

Only narrow to the workflow when the decided route identifies that workflow.

A stale review must never swallow a new transaction such as:

~~~text
jajan gorengan 5k
~~~

---

## 8.5 Clarification Must Expose Only Missing Dimensions

Generic responses such as:

~~~text
Coba sebutkan nominal, waktu, dan jenisnya lebih jelas.
~~~

are prohibited when those facts are already supported.

If only category is undecided, response should be equivalent to:

~~~text
Rp5.000 · Gorengan · hari ini

Kategori kemungkinan Makanan & Minuman.

[Benar] [Pilih lain]
~~~

---

# 9. Bank Email Requirements

Bank Email should become one of the lowest-interaction sources because evidence arrives automatically.

## 9.1 Preserve Existing Evidence Verification

Keep bounded evidence verification for:

- transaction_observed
- amount_supported
- direction_supported
- channel_supported
- material_ambiguity

Preserve PR #120 semantics:

- routine security footer;
- customer-service number;
- transaction-history link;
- OTP/support wording;

must not independently invalidate a real completed transaction.

## 9.2 Deterministic Merchant Memory First

If an exact stored merchant/category rule exists and is auto-applicable:

~~~text
merchant rule
→ deterministic category
→ CONFIRMED
~~~

No Jev call is necessary.

## 9.3 New Merchant Category Uses Jev

Replace:

~~~text
new merchant
→ AMBIGUOUS_CATEGORY
→ review
~~~

with:

~~~text
new merchant
    ↓
active household categories
    ↓
Jev bounded category decision
    ↓
decisive
    ├─ yes → Go validation → CONFIRMED
    └─ no  → category-only review
~~~

Candidate categories are server-owned.

Jev chooses a bounded category slug/value.

Go resolves canonical category ID.

## 9.4 Missing Merchant Must Not Automatically Block Confirmation

If amount, supported time, transaction semantics, channel, and category are valid, merchant may remain NULL.

Merchant absence must not automatically create a required text field.

## 9.5 Never Invent Merchant

Generative LLM and Jev MUST NOT fabricate merchant text merely to satisfy a schema.

Unknown merchant is better represented as NULL than invented data.

## 9.6 Bank Review UX

If category is undecided:

~~~text
Rp53.000 · Debit Card
23 Sep 2026 · 13:45

Merchant tidak tersedia pada bukti.

Kategori kemungkinan:
Makanan & Minuman

[Benar] [Pilih kategori lain]
~~~

Do not request amount/date again.

If category is decisive, no review is required.

---

# 10. Receipt Requirements

A clear receipt is evidence of a transaction.

Failure to find an existing duplicate is NOT itself uncertainty.

## 10.1 Remove Review-by-Default

Current conceptual anti-pattern:

~~~text
extract receipt
→ find existing match

one strong match
→ link evidence

otherwise
→ create review
~~~

must be replaced.

## 10.2 Required Pipeline

~~~text
receipt/image
    ↓
generative extraction
    ↓
field validation
    ↓
bounded repair if needed
    ↓
duplicate/reconciliation search
    ↓
if exact strong existing match:
    link evidence
    stop

otherwise:
    bounded semantic decision
        transaction validity
        amount support
        date support
        category
        material ambiguity
    ↓
Go canonical validation
    ↓
safe
    ├─ yes → create CONFIRMED transaction
    └─ no  → review only unresolved dimensions
~~~

## 10.3 New Receipt Auto-Confirm Conditions

At minimum, policy must establish:

- valid positive amount;
- expense semantics;
- supported transaction date according to source policy;
- valid category;
- no material evidence conflict;
- no unresolved plausible duplicate.

Merchant remains optional.

Deterministic arithmetic validation remains authoritative where applicable.

## 10.4 Generative Confidence Is Not Sole Gate

A confidence >= threshold check MUST NOT independently authorize canonical mutation on Jev-enabled workflows.

## 10.5 Receipt Review Examples

Only category uncertain:

~~~text
Rp87.500 · Superindo
22 Sep 2026

Richmod belum bisa memilih:

[Belanja Rumah]
[Makanan & Minuman]
~~~

Only date missing:

~~~text
Rp87.500 · Superindo
Belanja Rumah

Tanggal transaksi tidak tercantum.

[Tanggal: ______]
~~~

Duplicate ambiguity:

~~~text
Receipt ini mungkin cocok dengan:

[22 Sep · Rp87.500 · Superindo]
[23 Sep · Rp87.500 · Superindo]

[Pilih transaksi]
~~~

---

# 11. Transaction Screenshot Requirements

Transaction screenshots may contain multiple financial events.

The unit of certainty is each row, not the entire image.

## 11.1 Remove Review-by-Default for Unmatched Rows

Current conceptual anti-pattern:

~~~text
row matched
→ merge/link

row unmatched
→ NEEDS_REVIEW
~~~

An unmatched row often means "new transaction", not "ambiguous transaction".

## 11.2 Required Per-Row Pipeline

~~~text
extract row
    ↓
validate fields
    ↓
reconciliation search
    ↓
bounded semantic decision
    ↓
Go validation
    ↓
AUTO_CONFIRM | MERGE | MINIMAL_REVIEW
~~~

## 11.3 Batch Jev Where Practical

Codex SHOULD avoid unnecessary one-request-per-row overhead.

When protocol and request limits permit, multiple row decisions may share one System One request.

Correctness and stored provenance remain row-specific.

Provider failure must never result in unsafe auto-confirm.

## 11.4 Required User Summary

Example:

~~~text
10 transaksi ditemukan.

✓ 8 berhasil dicatat
✓ 1 cocok dengan transaksi yang sudah ada
! 1 butuh keputusan kategori
~~~

Do not create ten review cards when only one row is uncertain.

## 11.5 Incoming Rows Remain Sensitive

When evidence cannot distinguish income from own-account/household transfer, human classification may remain required.

Do not trade correctness for review reduction.

---

# 12. Financial Provider Email Partial Resolution

The backend already resolves source account and wealth account independently.

The UX must preserve that partial knowledge.

Example:

~~~text
Funding Account:
Bank Jago
→ resolved

Provider Wealth Account:
Bibit Growth
→ unresolved
~~~

The review should request only:

~~~text
Wealth Account tujuan:
[select]
~~~

It MUST NOT ask the user to select Bank Jago again.

Resolution APIs must accept only unresolved entities.

Backend combines persisted known facts with newly supplied missing values and validates the complete result.

---

# 13. Review Inbox Redesign

The Review Inbox must move from **form-first** to **proposal-first**.

## 13.1 Default Card Anatomy

A review card should communicate:

~~~text
WHAT HAPPENED
known financial facts

WHAT RICHMOD THINKS
proposed decision

WHY IT NEEDS YOU
specific unresolved dimension

PRIMARY ACTION
one-click acceptance or bounded choice

SECONDARY ACTION
Change / inspect evidence
~~~

## 13.2 Example

Desired:

~~~text
Rp54.000 · Indomaret
23 Sep 2026 · 14:32

Richmod thinks:
Makanan & Minuman

Why this needs you:
Category decision is still ambiguous.

[✓ Benar] [Pilih lain]

Bukti ›
~~~

Not:

~~~text
Merchant [Indomaret           ]
Category [Makanan ▼           ]
Note     [                    ]

[Confirm]
~~~

## 13.3 Edit-on-Demand

Full editing controls should appear only after actions such as:

- Change;
- Edit;
- Choose another;

or when a genuinely missing free-form field requires entry.

## 13.4 Render Inputs from missing_facts

If:

~~~json
{
  "missing_facts": ["category"]
}
~~~

the card must not render merchant, amount, transactionAt, account, or note as required inputs.

## 13.5 Optional Note

Optional note input should not compete visually with the primary unresolved decision.

Prefer secondary edit mode.

## 13.6 Evidence Is Secondary

Evidence remains accessible, but the default interaction must answer:

> What exactly does Richmod need me to decide?

---

# 14. Telegram Review UX

The same ReviewDecision semantics must drive Telegram and Web.

Do not maintain two conflicting review models.

Example:

~~~text
Rp53.000 · Starbucks
23 Sep · 13:45

Kemungkinan kategori:
Makanan & Minuman

[✓ Benar]
[Pilih lain]
~~~

A natural-language reply such as "transport" to an explicitly bound category review should resolve that exact review when Transport is an allowed category.

---

# 15. Human Policy Choices That Remain Human

This initiative does not eliminate all decisions.

The following MUST NOT be silently inferred merely to reduce RHICE.

## 15.1 Primary Salary Designation

The first unknown salary source may still require:

~~~text
PRIMARY_SALARY
vs
ORDINARY_INCOME
~~~

## 15.2 Salary-Cycle Residual Allocation

Allocation of retained money across cash, mutual fund, gold, RDN, or another wealth account remains household intent.

Do not automatically allocate residuals from historical patterns unless a later product contract explicitly introduces such rules.

## 15.3 Unknown Transfer Ownership

If deterministic evidence cannot establish:

- OWN_ACCOUNT;
- HOUSEHOLD_ACCOUNT;
- INVESTMENT;
- EXPENSE;

the user remains authoritative.

## 15.4 Historical Correction

Changes to already canonical historical transactions remain auditable and deliberate according to current correction policy.

---

# 16. Review Reason Model

Do not collapse all uncertainty into a small generic set such as AMBIGUOUS_CATEGORY, UNKNOWN_MERCHANT, and UNKNOWN_PURPOSE.

ReviewDecision must preserve precise unresolved conditions.

Suggested conceptual reason codes:

- MISSING_TRANSACTION_DATE
- MISSING_AMOUNT
- AMBIGUOUS_CATEGORY
- EVIDENCE_CONFLICT
- POSSIBLE_DUPLICATE
- TRANSFER_RELATIONSHIP_UNKNOWN
- FINANCIAL_EMAIL_ACCOUNT_UNRESOLVED
- FINANCIAL_EMAIL_WEALTH_ACCOUNT_UNRESOLVED
- PRIMARY_SALARY_POLICY_CHOICE
- RESIDUAL_ALLOCATION_REQUIRED
- CORRECTION_CONFIRMATION
- DOCUMENT_EXTRACTION_INCOMPLETE
- SEMANTIC_DECISION_UNDECIDED

Existing reason codes may remain for compatibility, but the new decision object must expose the exact missing or conflicting dimensions.

---

# 17. Auto-Confirmation Policy

Every auto-confirm path MUST be explainable.

A canonical event may auto-confirm only when:

~~~text
all required canonical fields are present
AND
all required semantic decisions are decisive
AND
deterministic validation passes
AND
there is no unresolved duplicate ambiguity
AND
there is no unresolved evidence conflict
AND
there is no explicit human-policy choice
~~~

Every AI-assisted auto-confirm must retain decision provenance.

---

# 18. Source-Specific Required Fields

Do not implement one global rule that all transactions require merchant, date, category, and every other enrichment field.

Requirements differ by source/event type.

## 18.1 Expense

Typically requires:

- positive amount;
- expense semantics;
- supported transaction date/time according to source policy;
- valid expense category.

Merchant may be absent.

## 18.2 Income

Category is not necessarily required unless the current canonical model specifically requires it.

Income-vs-transfer classification may remain required.

## 18.3 Bank Email Time Provenance

If trusted email-received time is intentionally allowed as a fallback, preserve provenance explicitly, equivalent to:

~~~text
transaction_time_source = EMAIL_RECEIVED_AT
~~~

Do not represent a fallback time as if it was printed in the source.

## 18.4 Receipt Date

A missing receipt transaction date must not silently become upload time unless an explicit source policy permits this.

If unsupported and required, ask only for date.

---

# 19. Learning and Memory

Richmod should reduce future interaction through deterministic learning.

However:

~~~text
learning != silent assumption
~~~

Existing merchant-learning consent semantics remain.

Example:

~~~text
Starbucks → Makanan & Minuman
~~~

Richmod may offer:

~~~text
Ingat kategori ini untuk Starbucks?
[Ya] [Tidak]
~~~

Once an explicit auto-apply rule exists:

~~~text
future Starbucks transaction
→ deterministic category
→ no Jev
→ no review
~~~

This is desired.

---

# 20. API Requirements

Review APIs must expose enough information for proposal-first clients.

Conceptual shape:

~~~json
{
  "id": "...",
  "status": "OPEN",
  "decision": {
    "decisionClass": "EVIDENCE_GAP",
    "reasonCode": "AMBIGUOUS_CATEGORY",
    "knownFacts": {
      "amountIdr": "53000",
      "transactionAt": "2026-09-23T13:45:00+07:00"
    },
    "proposedFacts": {
      "categorySlug": "food-and-drink"
    },
    "missingFacts": ["category"],
    "boundedChoices": [],
    "whyNotAutoConfirm": "category judgment was undecided",
    "allowedActions": [
      "ACCEPT_PROPOSAL",
      "SET_CATEGORY",
      "IGNORE"
    ]
  }
}
~~~

Exact wire naming may follow current API conventions.

The semantics are mandatory.

## 20.1 Resolution Endpoints

Resolving category must not require the client to resubmit known amount, timestamp, merchant, account, or description.

The backend loads persisted known facts, merges only supplied missing facts, then validates the resulting canonical decision.

---

# 21. Persistence and Audit

Every automatically confirmed AI-assisted mutation MUST retain:

- source event;
- original evidence references;
- extraction provenance;
- Jev decision provenance when used;
- policy version;
- canonical mutation audit entry.

Existing judgment_decision provenance requirements in ADR-038 remain binding.

Never store provider secrets or credentials in decision metadata.

---

# 22. Product Telemetry

Existing model-layer telemetry remains useful:

- JEV_ONLY
- JEV_THEN_GENERATIVE
- GENERATIVE_ONLY
- native_tool_calls_avoided

Add product telemetry.

## 22.1 Per Canonical Event

Track:

- source_type;
- auto_confirmed;
- review_created;
- required_user_inputs;
- typed_fields;
- bounded_choices;
- review_round_trips;
- decision_class;
- review_reason.

## 22.2 Review Resolution

Track:

- review_accepted_without_edit;
- review_accepted_with_edit;
- review_rejected;
- fields_changed;
- time_to_resolution;
- round_trip_count.

## 22.3 Auto-Confirm Correction

Track later material corrections by:

- source_type;
- decision_policy_version;
- decision_source;
- correction_field.

## 22.4 Required Derived Metrics

Dashboards/log queries must support:

- RHICE;
- typed fields / canonical event;
- human-touch rate;
- review rate by source;
- review rate by reason;
- approval-without-edit rate;
- auto-confirm correction rate;
- review round trips;
- time to canonical state.

A high review_accepted_without_edit rate is a product signal that the system is over-reviewing.

---

# 23. Semantic Canary Corpus

PR #120 showed that prompt-text regression tests do not fully protect against semantic provider drift.

Create a staging/smoke semantic corpus for important Jev behavior.

It SHOULD NOT be a flaky mandatory unit test on every local run.

It SHOULD be runnable:

- manually;
- in staging;
- before relevant production promotion;
- periodically where operationally practical.

Bank Email corpus should include:

- real completed Jago transaction with security footer;
- support phone number;
- transaction-history link;
- promo/cashback email;
- monthly statement;
- two plausible transaction amounts;
- wrong channel;
- wrong direction.

Assert expected bounded outcomes.

Over time add fixtures for:

- receipt semantics;
- category decisions;
- transfer-purpose decisions;
- review actions.

---

# 24. Required Telegram Acceptance Tests

## T1 — Simple Expense

Input:

~~~text
jajan gorengan 5k
~~~

When bounded decisions are decisive:

Expected:

~~~text
one CONFIRMED expense
zero review
zero manually entered fields
~~~

## T2 — Open Implicit Review Must Not Swallow New Expense

State: one open review.

Input:

~~~text
jajan gorengan 5k
~~~

Expected:

~~~text
new expense processed normally
existing review remains unresolved
~~~

## T3 — Exact Review Reply

User replies directly to a category-review Telegram message:

~~~text
makanan
~~~

Expected:

~~~text
exact target review resolved
no generic review listing
no generic route rejection
~~~

## T4 — CREATE_TRANSFER Route

Jev route: CREATE_TRANSFER.

Expected: falls through to the appropriate agent/tool workflow.

Must not return generic clarification.

## T5 — SEARCH_TRANSACTIONS Route

Expected: agent/tool search lane, not generic clarification.

## T6 — CORRECT_TRANSACTION Route

Expected: correction/search workflow, not generic clarification.

---

# 25. Required Bank Email Acceptance Tests

## B1 — Learned Merchant

Known merchant with stored auto-apply category.

Expected:

~~~text
CONFIRMED
RHICE = 0
~~~

## B2 — New Merchant, Decisive Category

Expected:

~~~text
CONFIRMED
no review
~~~

## B3 — New Merchant, Category Undecided

Expected:

~~~json
{
  "missing_facts": ["category"]
}
~~~

No amount/date re-entry.

## B4 — Merchant Absent, Other Facts Valid

Merchant may remain NULL.

Merchant text input must not be mandatory merely because the source omitted it.

## B5 — Genuine Two-Amount Ambiguity

No auto-confirm.

Review exposes amount conflict.

## B6 — Jev Provider Failure

No unsafe auto-confirm.

No silent generative semantic fallback.

---

# 26. Required Receipt Acceptance Tests

## R1 — Clear New Receipt

Expected:

~~~text
CONFIRMED transaction
no review
~~~

## R2 — One Strong Existing Match

Expected:

~~~text
link receipt evidence
do not create duplicate
~~~

## R3 — Multiple Plausible Matches

Expected:

~~~text
POSSIBLE_DUPLICATE review
bounded candidate choices
~~~

## R4 — Only Date Missing

Expected:

~~~text
single date input
~~~

No category, merchant, or amount re-entry.

## R5 — Only Category Undecided

Expected: category-only bounded review.

---

# 27. Required Screenshot Acceptance Tests

## S1 — Ten Clear New Rows

If all rows independently satisfy policy:

~~~text
10 CONFIRMED
0 review
~~~

## S2 — Eight Clear, Two Ambiguous

Expected:

~~~text
8 CONFIRMED
2 review
~~~

Each review includes only that row's unresolved dimension.

## S3 — Strong Existing Match

Link evidence; create no duplicate.

## S4 — Row Missing Date Only

Expected: date-only review.

---

# 28. Required Financial Provider Email Acceptance Tests

## F1 — Source Resolved, Wealth Account Unresolved

Review requests only wealth account.

## F2 — Wealth Account Resolved, Source Unresolved

Review requests only source account.

## F3 — Both Resolved

No entity-resolution review if all other semantic validation passes.

---

# 29. Required Review UI Acceptance Tests

## U1

Given:

~~~json
{
  "missing_facts": ["category"]
}
~~~

render category interaction only.

Do not render merchant, amount, transactionAt, or account as required controls.

## U2

Given a proposed category, default UI presents:

~~~text
Accept
Change
~~~

not a complete edit form.

## U3

Selecting Change may reveal editable controls.

## U4

Known facts display read-only by default.

## U5

Web and Telegram resolution produce equivalent canonical state.

---

# 30. Backward Compatibility

Do not break:

- canonical transaction ledger;
- transaction evidence;
- reconciliation;
- merchant memory;
- salary events;
- salary-cycle behavior;
- wealth accounts;
- current confirmed transactions;
- audit history;
- existing resolved reviews;
- Bank Email ingestion;
- Financial Provider Email ingestion;
- Telegram durable ingress;
- Review Inbox routing;
- LiteRouter boundary;
- Jev decision provenance.

Existing open reviews must remain resolvable.

Migration may enrich them with the new decision contract where safe.

Never invent missing historical facts during migration.

---

# 31. Non-Goals

This initiative MUST NOT:

- turn Jev into canonical authority;
- let a generative model directly mutate PostgreSQL;
- lower thresholds solely to improve review metrics;
- fabricate merchants;
- auto-merge ambiguous duplicates;
- auto-classify unknown account ownership without supporting evidence;
- automatically choose primary-salary policy;
- automatically allocate salary residual;
- replace LiteRouter;
- call TypeSafe directly;
- introduce Python, Java, Ollama, Redis, Kafka, RabbitMQ, or a vector database;
- redesign unrelated dashboards;
- rewrite the canonical ledger;
- silently rewrite historical confirmed data.

---

# 32. Implementation Stages

**Implementation status (2026-09-24):** PR #121 is merged and remains the
normative product contract. Stages 1, 2, and 4–7 are implemented on current
`main`; Stage 3 is under review in PR #126. Stage 0 telemetry is implemented
on the current feature branch. Stage 8's canary is runnable, but threshold
calibration still requires sufficient production evidence. This is not a claim
that the full Definition of Done has passed.

## Stage 0 — Baseline and Telemetry

**Status (2026-09-24): implementation complete; production baseline capture pending.**
`GET /api/v1/operations/status` returns a read-only `product` rollup
(`apps/api/internal/operations/product.go`) derived entirely from canonical
state, so it cannot drift from the ledger and needs no new pipeline. It reports
source events with their processing states (processed/ignored/needs-review),
human-touch rate (distinct reviewed events over the whole window cohort, so a
review on a still-pending event cannot push it above 1), review rate by source,
review rate by reason, RHICE (`explicitInputs / canonicalEvents`), typed fields,
open reviews, accepted-without-edit, resolution time, Telegram review turns,
bounded choices, and the auto-confirm correction rate/field/source breakdown.
The RHICE denominator includes only confirmed transactions, not pending,
needs-review, or voided rows.

Definitions are pinned to the actions the writers actually emit for a canonical
transaction: explicit inputs are `CONFIRM_REVIEW`, `TELEGRAM_CONFIRMED`, `TELEGRAM_MERCHANT_DECISION`,
`TELEGRAM_TRANSFER_CLASSIFIED`, `TRANSFER_RECONCILED`,
`RECLASSIFIED_ASSET_PURCHASE`, `COMPLETE_BANK_FACTS`, `SET_PAY_DATE`,
`SET_FINANCIAL_EMAIL_ENTITIES`, `PRIMARY_SALARY`, `ORDINARY_INCOME`,
`MERGE_EXISTING`, and `CONFIRM_NEW_TRANSFER`; typed fields are the subset whose
action names a value the user entered (`COMPLETE_BANK_FACTS`, `SET_PAY_DATE`,
`SET_FINANCIAL_EMAIL_ENTITIES`). Residual allocations and wealth-only snapshot
confirmations remain review metadata, not inputs before a ledger transaction.
Reclassified Wealth observations join through their preserved transaction
evidence. System
resolutions (`EMAIL_RECEIVED_AT_FALLBACK`, `RECONCILED_TERMINAL_TRANSACTION`,
`LEGACY_TRANSACTION_RESOLVED`, `NO_LONGER_APPLICABLE`) and `IGNORE` are neither
an input nor a typed field, and accepted-without-edit is the resolution actions
that only accept a proposal (`CONFIRM_REVIEW`, `TELEGRAM_CONFIRMED`,
`TELEGRAM_MERCHANT_DECISION`). RHICE counts only recent resolutions linked to a
recent canonical transaction through its review binding or preserved evidence,
so the numerator and denominator share the same canonical-event cohort.

Review turns are recorded as privacy-bounded events: only action names and an
allow-listed set of changed field names are retained, never values or user text.
Auto-confirm corrections are captured atomically with the canonical update.
The correction rate uses the same 30-day auto-confirmed transaction cohort as
its denominator, and correction events retain source/policy/decision provenance.
Every terminal Telegram review action (plain confirm, plain reject, transfer
classification, merchant decision, bounded callback) records one bounded-choice
turn: the reply evidence carries an explicit `classification` marker so a
confirm/reject is never silently counted as zero, and the resolution trigger
skips only when that same turn was already recorded, which keeps a delayed or
out-of-order reply from double-counting one interaction.
`notYetMeasurable` keeps pre-migration history visible; it clears only after a
household has a full 30-day telemetry window. Older turns are not backfilled or
guessed.

Before materially expanding auto-confirmation:

- capture the current review rate, source, and reason distribution before
  enabling broader auto-confirmation;
- wait for the first complete telemetry window before interpreting trend data.

Exit criterion:

~~~text
RHICE and guardrail metrics are measurable.
~~~

## Stage 1 — Telegram Orchestration Correctness

**Status (2026-09-24): implemented on main.** Server-owned route lanes are
exhaustive, valid non-fast routes fall through to the conversational agent,
exact replies bind before route arbitration, and implicit bindings are
route-gated. See `judgment_route_lane_test.go` and
`agent_route_first_lane_test.go`.

Implement:

- exhaustive Jev route dispatch;
- agent fall-through for valid non-fast routes;
- exact workflow binding precedence;
- implicit binding route gating;
- missing-dimension-aware clarification.

Exit criteria:

- every route enum is covered by a lane test;
- exact reply tests pass;
- implicit open-review regression passes.

This is P0 and should ship before broad auto-confirm expansion.

## Stage 2 — ReviewDecision Foundation

**Status (2026-09-24): implemented on main.** The shared ReviewDecision
contract is persisted, exposed by the API, and populated from reason presets
with known/proposed/missing/conflicting facts, allowed actions, decision class,
and provenance. See ADR-039 and PR #136.

Introduce:

- ReviewDecision representation;
- persistence;
- API exposure;
- decision classes;
- known/proposed/missing/conflicting facts;
- reason/provenance.

Existing flows may first populate the contract without changing confirmation policy.

Exit criterion:

~~~text
every new review can explain exactly why user input is required
~~~

## Stage 3 — Bank Email Zero-Touch Expansion

**Status (2026-09-24): under review in PR #126.** Main's stable
`SPENDING_ONLY` path remains unchanged; the PR adds category-only review for
uncertainty, Jev category choices for new merchants, and one bounded retry for
unsupported evidence verdicts. Merge remains gated on Hermes review.

Implement:

- Jev category classification for new merchants;
- nullable merchant behavior;
- category-only review on uncertainty;
- preserve PR #120 evidence semantics.

Exit criterion:

clear ordinary bank transactions require zero user input.

## Stage 4 — Receipt Auto-Confirm

**Status (2026-09-23): implemented.** A clear new receipt (no candidate match,
resolved category, high confidence, consistent arithmetic) now confirms directly
instead of opening a review. Duplicate ambiguity still links evidence or opens a
POSSIBLE_DUPLICATE review, and receipts missing their date or category still ask
only for the unresolved fact. See ADR-041.

Implement:

- bounded post-extraction semantic decision;
- new transaction auto-confirm;
- exact residual review generation;
- duplicate safety.

Exit criterion:

clear new receipt no longer creates a review.

## Stage 5 — Screenshot Per-Row Auto-Confirm

Implement:

- per-row semantic validation;
- safe auto-confirm;
- batch processing summary;
- row-specific minimal review.

Status: implemented on `feat/screenshot-rows` (ADR-042). One batched bounded
category ruling per image; a row auto-confirms only with a decisive category, a
printed date, no source conflict, and high extraction confidence. Unresolved
rows store the PRD §7 decision and the document sends one batch summary.

Exit criterion:

clear rows do not create mandatory review cards.

## Stage 6 — Financial Email Partial Resolution

Implement:

- partial entity-resolution contract;
- partial resolution API;
- missing-entity-only UI.

Status: implemented on `feat/financial-email-partial` (ADR-043). A resolved
entity persists on the observation and is no longer requested; the review's
`missingFacts` names only the unresolved dimension, and the API merges persisted
known facts with the submitted values before validating the complete result.

## Stage 7 — Proposal-First Review Inbox

Refactor Review Inbox to render from ReviewDecision.

Default:

~~~text
proposal
→ Accept / Change
~~~

not:

~~~text
full form
~~~

Status: implemented on `feat/proposal-first-inbox` (ADR-044). The Inbox renders
the stored ReviewDecision — proposal, reason, one primary action — and renders a
required input only for a dimension the decision named in `missingFacts`. Known
accounts and Wealth Accounts are shown, never re-asked; full editing sits behind
an explicit edit step and evidence behind a secondary disclosure. Reviews created
before the decision contract existed keep rendering the full form.

## Stage 8 — Semantic Canary and Threshold Calibration

**Status (2026-09-24): corpus implemented; calibration pending production evidence.**
The optional Jev corpus is documented in the production deployment runbook and
must remain a manually triggered smoke check, not a flaky unit-test gate. Do not
lower thresholds until the review and auto-confirm correction metrics have a
representative production window.

After behavior ships:

- run semantic corpus;
- inspect review reasons;
- inspect accepted-without-edit rate;
- inspect auto-confirm correction rate.

Only then consider threshold calibration.

Threshold changes must be evidence-driven.

---

# 33. Rollout Safety

New auto-confirm behavior SHOULD have independent operational rollback/kill-switch controls consistent with current Richmod configuration patterns.

Recommended independent scopes:

- Bank Email category auto-confirm;
- receipt new-transaction auto-confirm;
- screenshot row auto-confirm.

One source should be disable-able without disabling all others.

---

# 34. Product Success Criteria

Functional success:

~~~text
clear transactions reach canonical state without user interaction
~~~

Bounded uncertainty:

~~~text
one unresolved choice
→ at most one user choice
~~~

Free-form uncertainty:

~~~text
only genuinely missing free-form values require typing
~~~

Correctness:

~~~text
no material increase in post-auto-confirm correction rate
~~~

Architecture:

~~~text
Go remains authority
Jev remains bounded
generative LLM remains extraction/reasoning layer
~~~

Review quality:

Every review must explain:

- what Richmod knows;
- what Richmod proposes;
- what is unresolved;
- why auto-confirm was blocked;
- exactly what the user must decide.

---

# 35. Production Evaluation

After sufficient post-release traffic exists, compare against pre-release baseline.

Primary metric:

**Required Human Inputs per Canonical Financial Event**

Supporting metrics expected to decline:

- human-touch rate;
- typed fields/event;
- review rate;
- review round trips;
- time to canonical state.

Guardrail expected to remain stable:

**Post-Auto-Confirm Material Correction Rate**

Richmod may have low personal-household event volume. Do not draw conclusions from tiny samples.

When production volume is insufficient, rely on deterministic scenario acceptance tests while continuing to collect telemetry.

---

# 36. Product Decision Examples

## Example A — Telegram

User:

~~~text
jajan gorengan 5k
~~~

Ideal:

~~~text
✓ Rp5.000 · Gorengan
Makanan & Minuman · hari ini

Sudah dicatat.
~~~

Required follow-up input: 0.

## Example B — Only Category Uncertain

Known:

~~~text
Rp5.000
Gorengan
today
expense
~~~

Richmod:

~~~text
Rp5.000 · Gorengan

Kemungkinan kategori:
Makanan & Minuman

[✓ Benar]
[Pilih lain]
~~~

Do not ask amount/date/type again.

## Example C — Bank Jago Without Merchant

Evidence:

~~~text
Rp53.000
OUTGOING
DEBIT_CARD
13:45
merchant unavailable
~~~

If category is supported:

~~~text
auto-confirm
merchant = NULL
~~~

No merchant form.

## Example D — Receipt

Receipt contains:

~~~text
Indomaret
57,500
23 Sep 2026
valid arithmetic
clear category
no duplicate
~~~

Expected:

~~~text
CONFIRMED
~~~

Not:

~~~text
NEEDS_REVIEW because no existing match exists
~~~

## Example E — Screenshot

Screenshot contains 18 rows.

Result:

~~~text
✓ 16 recorded automatically
✓ 1 merged with existing evidence
! 1 needs category confirmation
~~~

Only one user decision.

## Example F — Financial Provider Email

Known:

~~~text
source = Bank Jago
wealth target unresolved
~~~

UI:

~~~text
Wealth Account:
[select]
~~~

Do not ask Bank Jago again.

---

# 37. Definition of Done

## Architecture

- [ ] valid Jev routes are exhaustively dispatched;
- [ ] exact bindings outrank generic route classification;
- [ ] implicit bindings remain route-gated;
- [ ] Go remains canonical authority.

## Review Contract

- [ ] ReviewDecision exists;
- [ ] known facts persist;
- [ ] proposed facts persist;
- [ ] missing facts persist;
- [ ] conflicting facts can be represented;
- [ ] why-not-auto-confirm is available;
- [ ] provenance is retained.

## Bank Email

- [ ] new merchants can be categorized by Jev;
- [ ] decisive category can auto-confirm;
- [ ] merchant is not universally mandatory;
- [ ] unknown merchant is never fabricated;
- [ ] PR #120 boilerplate semantics remain protected.

## Receipt

- [ ] clear new receipt can auto-confirm;
- [ ] duplicate ambiguity remains guarded;
- [ ] review asks only unresolved facts.

## Screenshot

- [ ] unmatched rows do not automatically become reviews;
- [ ] each row can auto-confirm independently;
- [ ] user receives aggregate processing summary.

## Financial Provider Email

- [ ] independently resolved entities are preserved;
- [ ] UI asks only unresolved entity.

## UX

- [ ] Review Inbox is proposal-first;
- [ ] forms are edit-on-demand;
- [ ] known values are never mandatory re-entry;
- [ ] Telegram follows the same ReviewDecision semantics.

## Observability

- [ ] RHICE measurable;
- [ ] typed-fields/event measurable;
- [ ] review rate by reason/source measurable;
- [ ] accepted-without-edit measurable;
- [ ] auto-confirm correction measurable;
- [ ] semantic canary is documented and runnable.

## Safety

- [ ] threshold lowering is not the primary review-reduction mechanism;
- [ ] no generative fallback becomes bounded semantic authority;
- [ ] no ambiguous duplicate auto-merge;
- [ ] no fabricated merchant;
- [ ] no silent human-policy decisions.

---

# 38. Final Product Principle

The intended Richmod experience is:

~~~text
User provides evidence or intent once.

Richmod does the work:
extract
understand
classify
validate
reconcile
remember
record

If Richmod can safely know the answer:
do not ask.

If Richmod can narrow the answer:
ask one bounded question.

If Richmod genuinely cannot know:
ask only for the missing fact.

If the decision is fundamentally human policy:
let the human decide.

Never make the user repeat information the system already knows.
~~~

The target is not:

~~~text
AI-assisted data entry
~~~

The target is:

~~~text
autonomous financial bookkeeping
with deterministic financial authority
and human intervention only at the irreducible boundary
~~~
