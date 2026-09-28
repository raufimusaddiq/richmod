# CORE-INTELLIGENCE-BOUNDARY REPAIR — production finding

**Status:** audit recorded; fix in the same branch
**Trigger:** real owner-household observation, 2026-09-28
**Audited main:** `f589946`
**Precedence:** SAVR PRD > BDR-004 > ADR-047 > ADR-048 > routing PRD #144 > ADR-045 > UIR

The product invariant violated:

```
Intelligence decides meaning. Go protects household ownership and canonical state.
```

Go had become a second semantic model on two surfaces. This document records the
findings, the classification, and the minimum change set.

---

# 1. Production finding (Telegram)

```
user: "halo"
  -> Jev route undecided / OTHER_OR_UNCLEAR
  -> Go canned "Permintaannya belum cukup jelas"  (NO conversational agent)

user: "aku ga bisa chat aja?"
  -> Jev provider failure
  -> Go readOnlyFallbackRequest() = strings.Contains keyword NLP
  -> not whitelisted -> NEEDS_REVIEW "layanan keputusan sedang tidak tersedia"
```

Both outcomes violate the PRD: a failed or undecided Jev route must not imply
that the user's sentence is unclear, and a machine/provider failure must not
become human work (SAVR PRD §3.3, §10.3; BDR-004; ADR-048).

## 1.1 Semantic ownership audit — Telegram text turn

| Decision | Location | Classification | Action |
| --- | --- | --- | --- |
| `readOnlyFallbackRequest` keyword intent NLP | `judgment_fast_path.go:191` | SEMANTIC INTERPRETATION | remove; replaced by server-owned tool capability |
| `degradeWithoutJudgment` mutation→NEEDS_REVIEW on provider failure | `judgment_fast_path.go:181` | MACHINE FAILURE AS HUMAN WORK | remove; drop to agent; capability policy already withholds mutation tools when Jev unconfigured |
| `OTHER_OR_UNCLEAR` route lane → clarification terminal | `judgment_route_lane.go:74` | SEMANTIC-TERMINATION | lane → `laneAgentFallthrough` |
| lane table `laneClarification` value | `judgment_route_lane.go` | dead after above | delete |
| route accept-failure → clarification terminal | `judgment_fast_path.go:80` | SEMANTIC-TERMINATION | drop to agent (route not fabricated; `state.Route` stays empty) |
| `laneForRoute` unknown-route → fail closed | `judgment_fast_path.go:90` | CANONICAL SAFETY (wiring guard) | keep fail closed with READ-only agent tools |
| `harvestSimpleTransaction`, `simpleAmountPattern` | `judgment_fast_path.go` | EXACT DETERMINISTIC KNOWLEDGE | keep |
| `userTextSupportsDate` | `transaction_decision.go` | EXACT DETERMINISTIC KNOWLEDGE (user date provenance) | keep |
| `directAcceptanceDecision` structural gates | `transaction_decision.go:308` | CANONICAL SAFETY | keep |
| mutation route authorization (`agentRecordTransaction`) | `agent_mutations.go:45` | CANONICAL SAFETY | keep |
| exact reply/review binding precedence | `agent.go`, `agent_binding_context.go` | EXACT DETERMINISTIC KNOWLEDGE | keep (T9/T10) |
| READ/SIDE-EFFECT tool classification + degraded surface | `agent_registry.go` | CANONICAL SAFETY (capability boundary) | keep; this is the real boundary |

The tool surface already encodes the target architecture: when the judgment
plane is unconfigured, every SIDE_EFFECT tool is withheld and only READ tools
remain (`agent_registry.go` `readOnly`). The keyword list was redundant *and*
semantic; the capability boundary is the correct authority. A configured
provider may still fail for one turn, so this repair also withholds writes
for undecided/failed routes in `agent.go`; configuration alone is not authority.

---

# 2. Production finding (Email)

```
Jago debit-card email
  -> job SUCCEEDED, source FAILED
  -> extraction INVALID/REPAIR, no proposal/review/transaction
  -> replay reproduced "invalid transaction time" on both attempts

Bibit Rp4,000,000
  -> review parked; transfer_reconciliation_case missing; Ignore failed; DML needed
```

## 2.1 Semantic ownership audit — bank-email validation

| Decision | Location | Classification | Action |
| --- | --- | --- | --- |
| `parseStrictBankRFC3339` (strict RFC3339Nano only) | `bankemail/validator.go:122` | CANONICAL SAFETY (instant must be representable) | keep; ask extractor to repair the exact invalid field |
| field-presence / direction / channel / amount / missing-field shape checks | `bankemail/validator.go` | CANONICAL SAFETY (structural/schema validity) | keep |
| `applyEmailReceivedTimeFallback` | `bankemail/processor.go:530` | DETERMINISTIC_KNOWLEDGE (source policy) | keep; PRD §4.7 / §13.4 |
| `persistExtractionFailure(...,"INVALID","REPAIR")` on schema failure | `bankemail/processor.go:233` | infrastructure/repair state | keep; must not create review |
| missing amount/time → `reviewIncompleteExtraction` | `bankemail/processor.go:308` | material residual | keep |

Provider-email observation reviews are separate from the bank-email pipeline.
A `TRANSFER_CLASSIFICATION` observation may have no
`transfer_reconciliation_case`; Web Ignore previously called the
case-only reconciler. This is a CANONICAL LIFECYCLE ROUTING defect, not
evidence of a wrong semantic classification. The exact reason the Bibit email
entered review remains unproven. Ignore now routes observation-scoped
reviews to the existing household-scoped observation finalizer; case-backed
transfer reviews still use their reconciler.

The exact invalid model timestamp was **not captured** in the previous
read-only production replay. We cannot prove that it was unambiguous or
accepted; only that both extraction attempts failed the strict schema with
`invalid transaction time`. Silently broadening Go's date parsing, or
converting a malformed printed timestamp to received-at, would be guessing.
The retry prompt now carries the exact schema failure for the failing field
(`extractor.go` repair prompt; `validator_repair_test.go` asserts the repair
attempt receives "invalid transaction time"). It asks for RFC3339 with a
timezone offset, and a second invalid attempt still lands in repair status
rather than being converted to received-at.

---

# 3. Minimum change set

Telegram

1. `OTHER_OR_UNCLEAR` → `laneAgentFallthrough`; delete `laneClarification`.
2. Route accept-failure and unknown-route drop to the conversational agent
   instead of terminating (route not fabricated).
3. Delete `readOnlyFallbackRequest` and `degradeWithoutJudgment`; a provider
   failure always drops to the agent with per-turn READ-only capability unless
   exact server-bound review authority already exists.
4. `READ_*` lanes accept a semantically-unresolved period by dropping to the
   agent (read tools compute exact ranges), removing a second silent terminate.

Email

5. Keep `parseStrictBankRFC3339` strict. Feed the exact validation failure
   to the existing second extraction attempt. No provider-specific parser.
6. Route observation-scoped `TRANSFER_CLASSIFICATION` Ignore to the existing
   observation finalizer instead of requiring a nonexistent transfer case.

No new table, keyword list, router, or framework (SAVR PRD §16).

---

# 4. Preserved (do not regress)

- PR #144 simple-transaction Jev-only fast path and its call budget.
- PR #144 complex-transaction single-pass generative path, no full Jev replay.
- UIR exact reply/callback/workflow binding precedence and shared finalizer.
- Household/canonical safety: auth, ownership, IDs, active entities, amounts,
  idempotency, concurrency, first-write-wins, provenance/audit.

---

# 5. Regression matrix

See regression tests for the Telegram route, capability boundary, existing
single-pass budgets, review binding, and the bank-email schema repair.

---

# 6. Deployment and observation status

**Implementation:** PR #214 merged to `main` as `f702085`
(`--merge` commit) after Hermes `APPROVE` on `cfba8ea`; CI and
`Release Images` succeeded for `f702085`.

**Production:** `Deploy Production` run
[36456564122](https://github.com/raufimusaddiq/richmod/actions/runs/36456564122)
deployed `f702085` with human approval of the GitHub `production`
Environment. Observed after the run:

- `api`, `worker`, `web` containers run
  `ghcr.io/raufimusaddiq/richmod-*:sha-f70208537746a0725750dee53d68f78f55d4b127`;
- migrations already at version `72` (`goose: no migrations to run`);
- `GET /healthz` and `GET /readyz` both returned HTTP `200`.

**Owner-household observation: PARTIAL.** Aggregate-only checks on 2026-09-29,
after deployment, observed eight Telegram user turns, seven completed
`PROCESS_TELEGRAM_TEXT` jobs and seven successful send jobs. Of the six
post-deploy undecided route outcomes, all six used `JEV_THEN_GENERATIVE`;
two decisive routes used `JEV_ONLY`. One native READ call
(`query_spending`) occurred. There were zero post-deploy review items,
zero `judgment_decision` rows, no "belum cukup jelas" / unavailable-service
canned response, and zero RHICE product telemetry events. One generative call
timed out; its job retried and succeeded, without creating review work. Health
and readiness returned HTTP 200; worker logs had no persistent error.

This verifies real ordinary Telegram fallthrough, a canonical READ tool, and
no human work from route uncertainty in the observed turns. It does **not**
prove provider outage behavior, Jev-authorized mutation failure, fast-path
call-budget comparison, correction-rate stability, or email behavior. No
post-deploy email event was observed, so the Jago/Bibit production rechecks and
email-origin review projection remain `PRODUCTION_UNOBSERVED`. No synthetic
or seeded financial data was used.

**Closure gate:** Telegram observation is partial; UISC-03 acceptance and
UISC-04 freeze remain blocked until remaining production checks and email
observation are recorded. Do not start CEU.
