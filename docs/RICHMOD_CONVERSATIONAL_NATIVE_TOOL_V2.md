# Richmod Conversational Native Tool V2 — Codex Implementation Plan

**Repository:** `raufimusaddiq/richmod`  
**Baseline branch:** `main`  
**Reviewed baseline commit:** `9b73b98bda1b3b7fe95af53669f7b1a18308f55c` (`merge: native document and insight tools`, 2026-09-01)  
**Primary objective:** standardize Richmod's conversational and LLM architecture around provider-native tool calls, persistent Telegram turn context, deterministic domain execution, and consistent review orchestration.  
**Hard product constraint:** when Richmod uses an LLM, the model output MUST be a provider-native tool/function call. Do not use structured JSON-in-text responses as a production LLM contract.

---

## 0. Instructions to Codex

Implement this as a dedicated architecture refactor. Keep scope limited to the existing Richmod finance domains and infrastructure already present in the repository. Do not introduce unrelated new product domains as part of this work.

### Mandatory working rules

1. Start from the latest `main`, and confirm the baseline has not materially moved from the commit above. If it has moved, rebase the plan onto the new code but preserve the invariants below.
2. Do not rewrite or delete applied migrations `00001`–`00038`. Add new migrations.
3. Do not delete historical ADRs. ADR history is evidence. Mark conflicting ADRs as `Superseded by ...` or `Accepted — amended by ...`.
4. Do not retain a hidden production fallback from native tools to `Structured(...)`.
5. Do not retain deterministic natural-language intent parsing as a fallback for LLM failure.
6. Telegram callbacks/buttons may remain deterministic because no LLM is involved. All **free-text finance understanding that uses an LLM** must use a required native tool call.
7. Go remains authoritative for:
   - household authorization;
   - canonical record lookup;
   - IDR arithmetic;
   - date boundary resolution;
   - category membership validation;
   - transaction/review mutation;
   - proposal/review state transitions;
   - audit logging;
   - reconciliation;
   - idempotency.
8. Never expose database credentials to the model.
9. Avoid model-facing canonical UUIDs when the server can bind the target from conversation state. Use server-scoped ephemeral references only when a user needs to disambiguate among multiple candidates.
10. Side-effecting tools terminate the model phase for that user turn. Do not allow an autonomous multi-tool chain to perform multiple financial mutations.
11. Preserve the canonical evidence/proposal/review model. This is an orchestration refactor, not a ledger rewrite.
12. Preserve IDR-only and Asia/Jakarta behavior.
13. Preserve household isolation and multi-member review semantics.
14. Add tests before removing old fallbacks.

---

# 1. Why this refactor exists

Richmod already has a strong deterministic finance core:

- evidence-first source events;
- transaction proposals;
- canonical transactions;
- universal review items;
- household authorization;
- deterministic PostgreSQL aggregation;
- audit logs;
- durable job queue;
- bank-email native extraction;
- native document classification;
- native background insight generation.

The remaining architectural inconsistency is the conversational layer.

The current Telegram implementation uses native finance tools only as a preferred path. It then falls back to structured-output extraction and, on some failures, a deterministic natural-language parser.

The current native tool execution also behaves mainly as an intent router:

1. the model selects a tool;
2. Go executes the tool;
3. Go immediately formats/sends a reply;
4. the model receives only `{"status":"handled"}`;
5. the model therefore does not receive the authoritative tool result as persistent conversational state.

Recent conversation context is also only a short list of prior **user** messages. It does not contain assistant replies, authoritative tool results, active review state, pending edit state, pending batch state, or salary-choice state.

As a result, Telegram can be correct at individual deterministic workflows while still feeling like a workflow bot instead of a coherent finance conversation.

This refactor changes the orchestration while retaining deterministic finance ownership in Go.

---

# 2. Non-negotiable architecture invariants

After this work, the following statements must be true.

## 2.1 Native-only LLM invariant

Every production LLM invocation in the finance system must satisfy:

```text
model invocation
    -> provider-native function/tool call
    -> strict provider/tool-name validation
    -> strict Go argument decoding
    -> deterministic Go domain validation
    -> deterministic domain execution or review
```

The following production path must no longer exist:

```text
model
    -> output_text JSON
    -> json.Unmarshal
    -> finance action
```

Specifically:

- no `Structured(...)` call in worker runtime finance code;
- no Responses `text.format = json_schema` finance contract;
- no Chat Completions JSON-in-content finance contract;
- no regex/natural-language fallback after a native tool failure.

## 2.2 One model decision per phase

The provider must return exactly one allowed tool call per model response.

Use:

```text
tool_choice = required
parallel_tool_calls = false
```

Unknown tools, zero calls, multiple calls, malformed arguments, or trailing JSON fail closed.

Do not use `MaxToolCalls: 4` for finance mutations.

For Telegram P0, use **one model tool decision per user text turn**. Persist its result for the next turn instead of building an autonomous four-step mutation loop.

## 2.3 Side effects are Go-owned

The model expresses user intent and bounded arguments. It cannot:

- execute SQL;
- choose arbitrary household IDs;
- choose arbitrary canonical transaction UUIDs from the database;
- bypass review;
- directly set canonical status;
- invent totals;
- perform arithmetic used as financial truth.

## 2.4 Conversation state is server-owned

The model receives a bounded, server-built `TurnContext`. It must not infer state only from raw chat history.

The server context must include, when applicable:

- recent user turns;
- recent assistant turns;
- previous authoritative tool-result summaries;
- exactly bound reply target;
- active canonical review(s);
- pending transaction correction;
- pending batch;
- pending salary choice;
- allowed categories;
- current Asia/Jakarta time;
- prior search/result references.

Internal canonical IDs are kept in server-private binding state.

## 2.5 Exact binding remains strongest, but not exclusive

Telegram callback and `reply_to_message_id` bindings remain authoritative when present.

However, free-text review follow-ups do **not** require the user to press Reply if the server can deterministically establish one unique active target.

Binding precedence:

1. exact callback binding;
2. exact `reply_to_message_id` review binding;
3. one active pending action for the user/chat;
4. one uniquely active canonical review relevant to the user/chat;
5. prior server-scoped result reference;
6. general finance conversation.

If there are multiple plausible targets, do not guess. Ask a clarification and expose bounded choices.

## 2.6 Canonical review remains independent of Telegram

`review_item` remains the canonical backlog.

`review_request` remains a Telegram delivery/conversation projection.

Conversational inference may bind a user turn to an existing canonical review, but must never create a second competing review for the same canonical subject.

---

# 3. Current code findings that this plan must fix

## 3.1 Telegram native tools are optional, not mandatory

**Reference:** `apps/worker/internal/telegram/processor.go`, `Processor.Process`

Current flow:

```text
NativeToolCall(... Required:false, MaxToolCalls:4)
    -> if not handled
Structured(...)
    -> if failed
deterministicTextExtraction(...)
```

Remove both fallback layers.

## 3.2 `deterministicTextExtraction` violates the intended model contract

**Reference:** `apps/worker/internal/telegram/processor.go`

Delete `deterministicTextExtraction` and its regex/keyword logic after native-only tests are in place.

A gateway outage must fail/defer safely; it must not silently switch the semantic engine.

## 3.3 Native tool results are not useful conversation state

**Reference:** `apps/worker/internal/telegram/processor.go`, native loop in `Processor.Process`

Current continuation sends:

```json
{"status":"handled"}
```

The tool executor itself sends the Telegram reply.

This means the model never receives or persists the authoritative result in a useful structured turn context.

Do not preserve this four-step loop.

## 3.4 `NativeFinanceTools()` has incomplete feature parity

**Reference:** `apps/worker/internal/telegram/tool_registry.go`

Current tools:

- `query_transactions`
- `create_transaction`
- `create_transaction_batch`
- `propose_transaction_edit`
- `confirm_edit`
- `cancel_edit`

Problems:

- create tools do not include category, description, note, or model confidence;
- category enum cannot be dynamically restricted to the household category slugs;
- no native insight action;
- no native help action;
- no native out-of-scope action;
- no native clarification action;
- no explicit free-text review resolver;
- no merchant-learning confirmation tool;
- no salary-choice resolver;
- custom query date ranges are not represented correctly.

Rebuild the catalog instead of mechanically converting Structured DTOs.

## 3.5 `propose_transaction_edit` schema and executor disagree

**References:**
- `apps/worker/internal/telegram/tool_registry.go`
- `apps/worker/internal/telegram/processor.go`, `executeNativeTool`

The schema permits a description-only edit, but executor logic requires a parseable `transaction_at`.

Fix by replacing the tool contract.

## 3.6 `confirm_edit`/`cancel_edit` require an ID that is ignored

**References:**
- `apps/worker/internal/telegram/tool_registry.go`
- `apps/worker/internal/telegram/processor.go`

`pending_action_id` is required in the model schema, but execution resolves the pending action by household/user/chat.

Remove the model-facing pending UUID. Use the active server-bound action.

## 3.7 Model-facing canonical transaction IDs are unnecessary

`propose_transaction_edit` currently accepts a canonical transaction UUID.

For conversational correction, prefer:

- an ephemeral conversation reference generated by Go; or
- a bounded search target that Go resolves uniquely.

Do not require the LLM to choose a database UUID.

## 3.8 Current conversation history is user-only

**Reference:** `apps/worker/internal/telegram/processor.go`, `recentConversation`

Current behavior:

- same chat;
- `TELEGRAM_TEXT` only;
- last five minutes;
- max twelve messages;
- only user payload text/caption.

It excludes:

- assistant replies;
- tool results;
- active state.

Replace it with a `TurnContextBuilder`.

## 3.9 Free-text review is reply-message dependent

**Reference:** `apps/worker/internal/telegram/review.go`, `processBoundReview`

The free-text path returns `false` when `ReplyToMessage == nil`.

This is the main conversational UX gap.

Keep exact reply binding as priority #1, but add deterministic context binding when exactly one relevant active review exists.

## 3.10 Review understanding still uses Structured or keyword heuristics

**Reference:** `apps/worker/internal/telegram/review.go`

Examples:

- `extractReview` -> `p.gateway.Structured(...)`
- `transferReviewIntent` -> keyword matching
- `incomeReviewIntent` -> keyword matching

Free-text review understanding must be moved into native tools.

Inline buttons may continue invoking deterministic handlers directly.

## 3.11 Telegram inline insight still uses Structured

**Reference:** `apps/worker/internal/telegram/assistant.go`, `replyCycleInsight`

Background insight already uses native `write_financial_insight`.

Remove the separate Structured Telegram insight path and use one canonical insight service/result.

## 3.12 Document classification is native, type-specific extraction is not

Already native:

- `apps/worker/internal/document/processor.go`
  - `classify_financial_document`

Still Structured:

- `apps/worker/internal/document/payslip.go`
- `apps/worker/internal/document/receipt.go`
- `apps/worker/internal/document/screenshot.go`

Convert them to required native extraction tools without changing deterministic validation/reconciliation semantics.

## 3.13 Bank email extraction is the strongest existing reference pattern

**Reference:** `apps/worker/internal/bankemail/extractor.go`

Use this as a design reference:

- required native tool;
- one call;
- strict Go validation;
- no canonical accounting decision in model;
- fail closed.

Do not copy its two-attempt schema retry into every interactive chat path automatically; Telegram latency requirements are different.

## 3.14 Gateway already supports required native calls

**Reference:** `apps/worker/internal/gateway/client.go`

Useful existing behavior:

- `tool_choice = required`;
- `parallel_tool_calls = false` when one call;
- strict allowed tool names;
- native Responses and Chat Completions adapters;
- bounded response parsing;
- redacted error handling.

Refactor the public contract; do not replace the whole gateway stack.

## 3.15 `Structured` remains a first-class gateway API

**Reference:** `apps/worker/internal/gateway/client.go`

After all runtime callers are migrated, remove `Structured` from worker runtime API so a future developer cannot accidentally reintroduce JSON-in-text finance extraction.

## 3.16 There is a second legacy API-side Structured LLM client

**Reference:** `apps/api/internal/llm/client.go`

Current API `cmd/api/main.go` does not wire this client.

After repository-wide reference verification, remove it or replace it with the common native gateway abstraction. Do not retain two incompatible LLM contracts before future feature work expands the LLM surface.

## 3.17 Telegram text is not isolated from DEFAULT work

**References:**
- `apps/api/internal/telegram/intake.go`
- `db/migrations/00028_job_lane_lifecycle.sql`
- `apps/worker/cmd/worker/main.go`

Current trigger maps:

- callback/send/edit/review-text -> `INTERACTIVE`
- long-running extraction/insight -> `BACKGROUND`
- ordinary `PROCESS_TELEGRAM_TEXT` -> `DEFAULT`

Add a `CHAT` lane.

## 3.18 Worker has one consumer per lane

**Reference:** `apps/worker/cmd/worker/main.go`

Create configurable concurrency for the CHAT lane without allowing chat LLM work to starve callbacks.

## 3.19 LLM observability cannot prove native-only execution

**References:**
- `db/migrations/00029_review_llm_cycle_contracts.sql`
- `apps/worker/cmd/worker/main.go`
- `apps/api/internal/admin/read.go`

`llm_call` currently records task/protocol/model/status/tokens/cost, but does not distinguish:

- native tool call;
- tool continuation;
- legacy structured output.

Add `call_kind` and `tool_name`.

Also ensure new calls populate `household_id`.

---

# 4. ADR disposition plan

Do not delete ADRs. Preserve the historical record.

Create three new ADRs:

- `ADR-030-native-only-model-tool-contract.md`
- `ADR-031-conversational-telegram-turn-and-review-binding.md`
- `ADR-032-telegram-chat-job-lane.md`

Then update existing ADR status/notes as follows.

| ADR | Action | Required change |
|---|---|---|
| ADR-005 Cloud LLM Gateway boundary | **AMEND** | Keep gateway/provider boundary. Replace “strict JSON Schema output/non-streaming” as universal contract with native required tools for finance. Mark `Accepted — amended by ADR-030`. |
| ADR-008 Telegram human-in-the-loop review | **SUPERSEDE** | Its rule that review replies bind to stored Telegram message ID “not inferred conversation context” conflicts with the new unique active-state resolver. Mark `Superseded by ADR-031`. |
| ADR-009 Generic finance document intake | **AMEND** | Preserve pipeline and validation. State that classification and all model extraction stages use required native tools. Mark `Accepted — amended by ADR-030`. |
| ADR-010 Transaction proposals before untrusted mutation | **AMEND** | Remove “fallback parsing” as a natural-language production path. Preserve proposal-before-canonical mutation. Mark `Accepted — amended by ADR-030`. |
| ADR-015 Aggregate-only LLM insights | **AMEND** | Preserve aggregate-only facts. Replace “gateway returns strict narrative schema” wording with native `write_financial_insight` arguments. Mark `Accepted — amended by ADR-030`. |
| ADR-018 Explicit merchant category learning | **AMEND** | Preserve explicit confirmation. Clarify free-text confirmation can bind via active conversation state; exact Telegram reply is not mandatory. Mark `Accepted — amended by ADR-031`. |
| ADR-020 Telegram finance assistant | **SUPERSEDE** | Its strict extraction/intent DTO architecture is replaced by native tool decisions + persistent turn context. Mark `Superseded by ADR-030 and ADR-031`. |
| ADR-022 Dedicated interactive callback lane | **NO CHANGE** | Already superseded by ADR-026. Keep historical status. |
| ADR-023 Universal review items | **KEEP** | Core decision remains correct. Add optional note `Clarified by ADR-031` if desired; do not supersede. |
| ADR-024 Native finance tool-call harness | **SUPERSEDE** | “May return native function_call” is too weak and its current tool-loop assumptions are replaced. Mark `Superseded by ADR-030`. |
| ADR-025 Config-driven bank email ingestion | **KEEP** | Native bank extraction is already aligned. |
| ADR-026 Durable Telegram ingress and isolated job lanes | **AMEND** | Add `CHAT`; ordinary Telegram text no longer belongs to DEFAULT. Mark `Accepted — amended by ADR-032`. |
| ADR-027 Single-protocol bounded LLM calls | **AMEND** | Keep single configured provider protocol and budgets. State finance model output is native-only and add call-kind observability. Mark `Accepted — amended by ADR-030`. |
| ADR-028 Canonical universal review orchestration | **AMEND/CLARIFY** | `review_item` remains canonical. `review_request` remains delivery projection, but Telegram free-text can bind through unique active review context. Mark `Accepted — clarified by ADR-031`. |
| ADR-029 S3-compatible off-host storage | **NO CHANGE** | Unrelated. |

### ADR convention

Do not use “Cancelled” for an ADR whose historical decision shipped.

Use:

```md
## Status

Superseded by ADR-031.
```

or:

```md
## Status

Accepted — amended by ADR-030.
```

and add an `Amendment`/`Supersession` note pointing to the replacement ADR.

---

# 5. New ADR-030 — Native-only model tool contract

Create `docs/adr/ADR-030-native-only-model-tool-contract.md`.

Its decision must contain at least:

1. All finance LLM invocations use native provider function/tool calls.
2. `tool_choice` is required for finance inference/generation.
3. Exactly one tool call is accepted per provider response.
4. Parallel tool calls are disabled.
5. No production Structured-output fallback exists.
6. No natural-language regex fallback exists after model failure.
7. Tool arguments are untrusted and validated in Go.
8. Canonical IDs are not model-facing unless there is no safer server binding; conversational targeting uses scoped ephemeral references.
9. Side-effecting calls terminate the model phase.
10. Native tool calls never directly execute database operations in the gateway.
11. Deterministic Go replies are allowed. “Native-only” constrains **LLM output**, not every response in the product.
12. If LLM-generated reply prose is added later, it must itself be returned through a required native rendering tool, never arbitrary output text.
13. Document classification, payslip extraction, receipt extraction, screenshot extraction, bank extraction, insight generation, Telegram free-text understanding, and review clarification all follow this contract.
14. Model/provider auxiliary prose is ignored and never parsed as finance data.
15. Operational telemetry records `call_kind` and `tool_name`.

---

# 6. New ADR-031 — Conversational Telegram turns and contextual review binding

Create `docs/adr/ADR-031-conversational-telegram-turn-and-review-binding.md`.

Decision:

1. Persist bounded assistant/tool context, not only user source events.
2. Build a server-owned `TurnContext` for every free-text Telegram turn.
3. Include canonical active states:
   - exact reply-bound review;
   - active review candidates;
   - pending correction;
   - pending batch;
   - pending salary choice;
   - recent search/result references.
4. Exact callback/reply bindings take precedence.
5. If there is exactly one active compatible review/pending action, a normal non-Reply text message may bind to it.
6. If multiple candidates exist, no mutation is allowed until clarified.
7. Model-facing entity references are ephemeral and scoped to household/user/chat/turn.
8. Free-text review intent uses native tool calls.
9. Buttons remain deterministic transport shortcuts and do not require LLM calls.
10. `review_item` is still canonical; conversation state never replaces canonical review state.
11. Assistant replies and tool-result summaries are available to future turns.
12. The model never sees server-private canonical target IDs.

---

# 7. New ADR-032 — Dedicated Telegram CHAT lane

Create `docs/adr/ADR-032-telegram-chat-job-lane.md`.

Decision:

1. Job lanes become:
   - `INTERACTIVE`: callback ACK/action, send, edit;
   - `CHAT`: free-text Telegram conversation;
   - `DEFAULT`: ordinary non-chat short work;
   - `BACKGROUND`: document/bank/vision/insight.
2. `PROCESS_TELEGRAM_TEXT` belongs to `CHAT`.
3. `PROCESS_TELEGRAM_REVIEW_TEXT` is deprecated; review text is ordinary Telegram text interpreted with server context.
4. CHAT has independent configurable worker concurrency.
5. INTERACTIVE is never occupied by an LLM call.
6. Chat LLM budget must leave time for Go validation/DB/reply enqueue.
7. Telegram typing action is emitted for an accepted chat job before model work.
8. Queue metrics/admin status expose CHAT independently.

---

# 8. Target Telegram architecture

Target flow:

```text
Telegram webhook
    |
    v
durable source_event + PROCESS_TELEGRAM_TEXT
    |
    v
CHAT worker
    |
    +--> authorization re-check
    |
    +--> TurnContextBuilder
    |      |- exact reply binding
    |      |- active review(s)
    |      |- pending action/batch/salary
    |      |- recent USER turns
    |      |- recent ASSISTANT turns
    |      |- previous TOOL result summaries
    |      |- ephemeral entity references
    |      `- allowed household categories
    |
    v
ToolCatalogBuilder(context)
    |
    v
NativeToolCall(required=true, exactly one)
    |
    v
strict call validator
    |
    v
ToolExecutor
    |
    +--> deterministic PostgreSQL query / proposal / review mutation
    |
    +--> ToolExecutionResult
    |
    v
persist tool-result turn + assistant reply turn
    |
    v
enqueue Telegram send
    |
    v
source_event terminal state
```

Important: do **not** continue the model automatically after a mutating tool.

---

# 9. Turn context model

Create new files in `apps/worker/internal/telegram/`:

- `turn_context.go`
- `turn_store.go`
- `tool_catalog_v2.go`
- `tool_executor_v2.go`
- optionally `reply_renderer.go`

Suggested Go model:

```go
type TurnContext struct {
    LanguageHint      string
    CurrentJakarta    string
    CurrentUserText   string
    RecentTurns       []PublicTurn
    ActiveReview      *PublicReviewContext
    ReviewCandidates  []PublicReviewContext
    PendingAction     *PublicPendingContext
    PendingBatch      *PublicPendingContext
    SalaryChoice      *PublicSalaryContext
    LastSearch        *PublicSearchContext
    CategorySlugs     []string
}
```

Keep a separate server-private structure:

```go
type BoundTurnContext struct {
    Public TurnContext

    HouseholdID string
    UserID string
    TelegramUserID int64
    ChatID int64

    ExactReviewID *string
    ReviewBindings map[string]string       // ephemeral ref -> canonical review ID
    TransactionBindings map[string]string  // ephemeral ref -> canonical transaction ID

    PendingActionID *string
    PendingBatchID *string
    SalaryChoiceID *string
}
```

Never marshal `BoundTurnContext` directly into the model request.

---

# 10. Conversation persistence

Add a new migration after the current migration head.

Recommended table:

```sql
CREATE TABLE telegram_conversation_turn (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES household(id),
    telegram_user_id BIGINT NOT NULL,
    telegram_chat_id BIGINT NOT NULL,
    source_event_id UUID REFERENCES source_event(id),
    role TEXT NOT NULL CHECK (role IN ('USER','ASSISTANT','TOOL')),
    message_text TEXT,
    tool_name TEXT,
    public_context_json JSONB,
    telegram_message_id BIGINT,
    delivery_status TEXT NOT NULL DEFAULT 'N/A'
      CHECK (delivery_status IN ('N/A','PENDING','SENT','FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
      (role='TOOL' AND tool_name IS NOT NULL)
      OR role<>'TOOL'
    )
);
```

Indexes:

```sql
(household_id, telegram_chat_id, created_at DESC)
(telegram_user_id, telegram_chat_id, created_at DESC)
(source_event_id)
```

Do not use this table as financial evidence. `source_event`/`transaction_evidence` remain evidence.

### Turn retention/context policy

For the model:

- load at most ~20 recent public turns;
- use a time horizon around 30–60 minutes for ordinary conversational references;
- active review/pending state is loaded independently and may survive much longer;
- bound exact reply references override age heuristics.

The full DB rows can be retained longer for debugging if privacy policy allows, but keep `public_context_json` bounded and avoid storing documents, email bodies, secrets, or arbitrary provider reasoning.

---

# 11. Ephemeral entity references

Do not expose canonical transaction IDs just so the model can say “the second one”.

Implement scoped references.

Option A — preferred: keep reference mappings in `public_context_json` plus server-private turn data.

Option B — if easier to validate safely, add:

```sql
CREATE TABLE telegram_turn_reference (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    turn_id UUID NOT NULL REFERENCES telegram_conversation_turn(id) ON DELETE CASCADE,
    ref_key TEXT NOT NULL,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('TRANSACTION','REVIEW')),
    entity_id UUID NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE(turn_id, ref_key)
);
```

Public model context may contain:

```json
{
  "ref": "tx_2",
  "label": "Pamella",
  "amount_idr": "125000",
  "transaction_at": "2026-08-31T14:20:00+07:00"
}
```

It must not contain the canonical transaction UUID.

A subsequent correction tool can accept `target_ref:"tx_2"`.

Go resolves that reference only if:

- same household;
- same Telegram user/chat scope;
- non-expired;
- entity still valid;
- the reference came from an eligible previous turn.

Cross-household/cross-chat references must fail closed.

---

# 12. Tool catalog V2

Replace the static current catalog with a context-aware builder.

Suggested signature:

```go
func BuildFinanceTools(ctx BoundTurnContext) []gateway.ToolDefinition
```

The exact set may depend on active state.

## 12.1 Always-available tools

### `record_transaction`

Arguments:

- `type`: `INCOME|EXPENSE`
- `amount_idr`: digits-only positive string
- `merchant`: nullable bounded string
- `category_slug`: nullable enum dynamically built from household categories
- `description`: nullable string
- `note`: nullable string
- `date_reference`: `TODAY|YESTERDAY|EXPLICIT`
- `explicit_date`: nullable `YYYY-MM-DD`
- `local_time`: nullable `HH:MM`
- `confidence`: number 0..1
- `category_confidence`: number 0..1

Do not ask the model to fabricate RFC3339 when a deterministic Jakarta date reference is safer.

### `record_transaction_batch`

Items use the same bounded fields as `record_transaction`.

Limit remains 10.

Batch still requires explicit user confirmation before canonical recording.

### `query_spending`

Arguments:

- `period`: enum:
  - `TODAY`
  - `THIS_WEEK`
  - `LAST_WEEK`
  - `THIS_MONTH`
  - `LAST_MONTH`
  - `CURRENT_CYCLE`
  - `PREVIOUS_CYCLE`
  - `CUSTOM`
- `from_date`
- `to_date`

Go validates custom range and maximum disclosure period.

### `query_cashflow`

Same period schema.

### `search_transactions`

Arguments:

- period fields;
- `search_text`;
- optional `limit` must be ignored or bounded by Go; preferred fixed max six.

Go generates ephemeral refs for results.

### `list_review_items`

No canonical IDs in model response context. Return bounded human labels + ephemeral refs.

### `get_finance_insight`

Arguments:

- period fields.

Use canonical insight generation/read path. Do not call the Telegram Structured insight function.

### `ask_clarification`

Arguments should be semantic, not arbitrary assistant prose:

- `topic` enum;
- `missing_fields` array of enum values;
- optional candidate ephemeral refs.

Go renders the message.

### `finance_help`

No arbitrary model-generated answer is necessary. Go sends supported examples.

### `finance_out_of_scope`

Arguments:

- `reason` enum such as `NON_FINANCE`, `INVESTMENT_ACTION_UNSUPPORTED`, `SYSTEM_REQUEST`, `UNSUPPORTED_LANGUAGE`.

Go renders a safe response.

## 12.2 Conditional tools

### `propose_transaction_correction`

Available for general chat.

Arguments:

- optional `target_ref`;
- optional bounded `search_text`;
- period fields;
- correction fields:
  - `transaction_at` represented by date_reference/explicit_date/local_time;
  - `description`;
  - `category_slug`.

Validation:

- target must resolve to exactly one household transaction;
- at least one correction field;
- no model-facing canonical UUID;
- create a pending action before mutation.

### `confirm_pending_action`

Expose only when a pending correction exists.

Arguments may be empty or contain `confirmation:true`.

The server already knows the pending ID.

### `cancel_pending_action`

Same pattern.

### `confirm_pending_batch`

Expose only when a pending batch exists.

### `cancel_pending_batch`

Expose only when a pending batch exists.

### `resolve_salary_choice`

Expose only when a salary choice is pending.

Arguments:

- `choice`: `PRIMARY|ORDINARY|IGNORE`

### `resolve_review`

Expose when an active review is uniquely bound.

Arguments vary by review semantics but must be bounded:

- `action` enum;
- `category_slug`;
- `merchant`;
- `description`;
- `pay_date`;
- optional `remember_merchant` only if valid at that stage.

If multiple reviews exist, accept an ephemeral `review_ref` only after it has been presented in the turn context.

### `resolve_merchant_learning`

Expose only for the explicit merchant-rule confirmation stage.

Arguments:

- `remember`: boolean.

Preserve ADR-018 deliberate confirmation.

---

# 13. Tool execution contract

Replace:

```go
executeNativeTool(...) (bool, error)
```

with a result-oriented executor.

Example:

```go
type ToolExecutionResult struct {
    ToolName       string
    Mutated        bool
    SourceStatus   string
    Reply          ReplySpec
    PublicContext  any
    References     []EntityReference
}
```

Executor responsibilities:

- validate authorization again;
- validate tool args;
- execute authoritative DB operations;
- produce authoritative facts;
- create proposal/review/pending rows;
- create audit log;
- return one deterministic reply specification;
- never call Telegram network directly;
- never call the LLM.

Processor responsibilities:

1. persist tool result turn;
2. persist/enqueue assistant reply;
3. mark source event terminal;
4. commit atomically when possible.

This separation makes conversation state testable without Telegram transport.

---

# 14. Remove the current autonomous tool-result loop

In `apps/worker/internal/telegram/processor.go` remove the current:

```text
for step := 0; step < 4; step++ {
    executeNativeTool(...)
    NativeToolResult(... {"status":"handled"})
}
```

For this phase:

- one free-text user turn -> exactly one native tool decision;
- executor result -> deterministic reply + persisted context;
- next user message -> next native decision.

`NativeToolResult` can be removed from Telegram.

If no production caller remains after the refactor, delete `NativeToolResult` from `apps/worker/internal/gateway/client.go` entirely.

Do not keep an unused “agent loop” abstraction because it will invite future unsafe multi-mutation behavior.

---

# 15. Gateway API refactor

Current `Gateway` interfaces frequently include `Structured`.

Replace with a native-only invocation API.

Recommended types:

```go
type Invocation struct {
    RequestID   string
    HouseholdID string
    Task        string
    SystemPrompt string
    Content     any
    Tools       []ToolDefinition
    Options     NativeToolOptions
}

type NativeToolOptions struct {
    Required bool
    ReasoningEffort string
}
```

For finance callers, `Required` should effectively always be true.

Prefer hard-enforcing one call rather than exposing `MaxToolCalls` as a finance-facing option.

Possible API:

```go
func (c *Client) NativeToolCall(
    ctx context.Context,
    invocation Invocation,
) (ToolCall, Metadata, error)
```

### `validateNativeCalls`

Change contract to reject `len(calls) != 1`.

Do not accept multiple calls and silently return the first.

### Strict argument helper

Add a reusable helper:

```go
func DecodeToolArguments[T any](call ToolCall, expectedName string) (T, error)
```

Requirements:

- exact tool name;
- valid JSON;
- `DisallowUnknownFields`;
- exactly one JSON value;
- caller performs domain validation afterward.

Use it for bank, documents, insights, and Telegram to remove duplicated decoder logic.

---

# 16. Remove Structured runtime LLM code

After all callsites are migrated:

1. remove `Structured` from worker gateway interfaces;
2. delete or make unreachable the worker `Structured` implementation;
3. delete tests that only validate structured finance output;
4. keep general JSON helpers only if used elsewhere;
5. remove `apps/api/internal/llm/client.go` if repository-wide references confirm it is dead.

Add a repository guard script, for example:

`scripts/check_native_only_llm.sh`

It should fail CI if runtime finance code contains:

```text
.Structured(
text.format
json_schema
```

Allow explicit test fixtures only when necessary.

Example check scope:

```bash
rg '\.Structured\(' apps/worker apps/api/internal \
  --glob '!**/*_test.go'
```

Expected result after completion: zero finance runtime matches.

---

# 17. Telegram `Processor.Process` refactor

Target order:

```text
load source event
decode update
re-authorize identity

if callback:
    deterministic exact callback processing
    return

if /start linking/help transport command:
    deterministic handling if no LLM is required
    return

build BoundTurnContext
persist USER turn/context reference
emit typing action
build context-sensitive tool catalog
required NativeToolCall
validate tool
execute tool
persist TOOL result
persist/enqueue ASSISTANT reply
finalize source event
```

Remove these pre-LLM natural-language handlers from the free-text path:

- `processPendingSalaryChoice`
- `processPendingEdit`
- `processPendingBatch`
- `processBoundReview` as a separate natural-language parser
- Structured extraction
- deterministic text extraction

Their **domain mutation helpers** can be retained/refactored and called by tool executor.

---

# 18. Review refactor

## 18.1 Keep deterministic callback functions

Retain/refactor:

- `processReviewCategoryCallback`
- `processReviewDetailCallback`
- stale callback handling
- exact review message lookup
- household/category validation

Buttons are explicit user actions and do not need LLM interpretation.

## 18.2 Remove free-text keyword intent parsing

Remove from free-text semantics:

- `transferReviewIntent`
- `incomeReviewIntent`

Do not use substring matching such as `istri`, `rekeningku`, `pengeluaran`, etc. as the semantic engine.

The equivalent intent is expressed through the native `resolve_review` tool.

## 18.3 Remove `extractReview(... Structured ...)`

Review classification becomes native.

The review context sent to the model should contain bounded facts:

```json
{
  "review_type": "TRANSFER_CLASSIFICATION",
  "state": "AWAITING_CATEGORY",
  "amount_idr": "125000",
  "merchant": "Pamella",
  "allowed_actions": [
    "EXPENSE",
    "OWN_ACCOUNT_TRANSFER",
    "HOUSEHOLD_TRANSFER",
    "INVESTMENT_TRANSFER",
    "IGNORE"
  ]
}
```

No canonical review or transaction UUID is needed when exactly one review is bound.

## 18.4 Contextual binding rules

Implement deterministic resolver:

```go
func ResolveActiveReviewContext(...) (...)
```

Behavior:

- exact reply-to match -> bind exact review;
- otherwise load OPEN canonical reviews deliverable/relevant to the same active member/chat;
- filter expired/resolved/stale subjects;
- if exactly one compatible review exists -> bind it;
- if zero -> general conversation;
- if more than one -> expose bounded candidates and prevent mutation until clarified.

Do not bind “the newest” review just because it is newest.

## 18.5 Merchant learning

Keep explicit opt-in.

Free text such as:

```text
iya ingat untuk merchant ini
```

may resolve through `resolve_merchant_learning` when that state is active, even without Telegram Reply.

Do not auto-create aliases from categorization.

---

# 19. Assistant/query refactor

`apps/worker/internal/telegram/assistant.go` should stop acting as an alternate Structured-intent router.

Refactor reusable deterministic query functions into domain/query helpers that return data rather than immediately finalizing a Telegram reply.

Example:

```go
type SpendingSummary struct {
    PeriodLabel string
    Total string
    TopCategory string
    TopAmount string
}
```

Then:

```go
func (p *Processor) QuerySpending(...) (SpendingSummary, error)
```

Reply rendering remains deterministic for P0.

This keeps latency to one LLM call per user turn.

### Follow-up context

After:

```text
User: pengeluaran bulan ini berapa?
```

persist a tool result like:

```json
{
  "tool": "query_spending",
  "period": "THIS_MONTH",
  "total_idr": "4200000",
  "top_category": "makanan-minuman",
  "top_amount_idr": "1200000"
}
```

and the assistant reply.

Then:

```text
User: yang paling besar merchant apa?
```

the next model receives that previous turn and can choose an appropriate search/query tool instead of losing the referent.

---

# 20. Canonical insight consolidation

Current duplicate paths:

- background `apps/worker/internal/insight/processor.go` -> native;
- Telegram `replyCycleInsight` -> Structured.

Remove the second LLM implementation.

Preferred path:

1. Telegram tool `get_finance_insight`;
2. Go resolves the requested period/cycle;
3. read a recent matching canonical insight if valid;
4. otherwise create/enqueue/generate through the canonical insight service;
5. return deterministic status/result to Telegram.

If synchronous generation is required for interactive UX, extract a reusable native `GenerateFromFacts` service from the insight package rather than re-implementing a Telegram prompt.

Keep ADR-015 aggregate-only restriction.

---

# 21. Document extraction migration

## 21.1 `payslip.go`

Replace:

```go
p.gateway.Structured(...)
```

with required native tool:

```text
extract_payslip
```

Use `payslipSchema()` as function parameters.

Keep unchanged:

- IDR validation;
- gross/net validation;
- allowance/deduction arithmetic;
- payroll-period parsing;
- pay-date rules;
- caption fallback;
- review/proposal behavior;
- salary-source/event semantics.

## 21.2 `receipt.go`

Replace Structured with:

```text
extract_receipt
```

Use `receiptSchema(slugs)` as native parameters.

Keep:

- dynamic household category enum;
- arithmetic validation;
- timestamp plausibility;
- matching/reconciliation;
- proposal/review behavior.

## 21.3 `screenshot.go`

Replace Structured with:

```text
extract_transaction_screenshot
```

Use `screenshotSchema(slugs)`.

Keep:

- one row per visible completed transaction;
- no row combination;
- payment-status behavior for invoice/bill;
- candidate matching;
- review semantics;
- incoming-transfer caution.

## 21.4 `document/processor.go`

Remove `Structured` from its `Gateway` interface once all type-specific extractors are migrated.

---

# 22. Job lane migration

Current `job.lane` CHECK only allows:

```text
INTERACTIVE
DEFAULT
BACKGROUND
```

Add `CHAT`.

Create a new migration; do not modify `00016`/`00028`.

Migration actions:

1. add `CHAT` to lane constraint;
2. add `job_chat_claim_idx`;
3. replace `enforce_job_lane()` mapping:
   - `PROCESS_TELEGRAM_CALLBACK`, `SEND_TELEGRAM_MESSAGE`, `EDIT_TELEGRAM_MESSAGE` -> `INTERACTIVE`;
   - `PROCESS_TELEGRAM_TEXT`, compatibility `PROCESS_TELEGRAM_REVIEW_TEXT` -> `CHAT`;
   - bank/document/image/payslip/receipt/screenshot/insight -> `BACKGROUND`;
   - other -> `DEFAULT`;
4. backfill PENDING/RUNNING Telegram text jobs to CHAT.

### Compatibility note

`PROCESS_TELEGRAM_REVIEW_TEXT` should be treated as compatibility-only and removed from producers. New free-text review messages use `PROCESS_TELEGRAM_TEXT`.

---

# 23. CHAT worker concurrency

Refactor `apps/worker/cmd/worker/main.go`.

Instead of one loop per lane, support:

```text
INTERACTIVE: 1 consumer
CHAT: configurable 2–4 consumers
DEFAULT: 1 consumer
BACKGROUND: 1 consumer initially
```

Environment example:

```text
TELEGRAM_CHAT_WORKERS=3
```

Bound it to a sane range.

Do not put LLM work onto INTERACTIVE.

### Chat budget

Current job budget and per-LLM attempt can consume the same 10 seconds.

Change to something like:

```text
PROCESS_TELEGRAM_TEXT job budget: 10s
LLM model budget: 7–8s
remaining: validation + DB + enqueue
```

Exact numbers may be tuned, but the inner model timeout must be lower than the outer job timeout.

---

# 24. Telegram typing indicator

Add to `apps/worker/internal/telegram/bot.go`:

```go
func (b *Bot) SendChatAction(ctx context.Context, chatID int64, action string) error
```

Use `typing` for CHAT work.

Failure is best-effort and must not fail the finance job.

Do not send typing for callbacks.

---

# 25. Telegram send/assistant-turn persistence

Extend `SendPayload` with an optional conversation turn ID:

```go
ConversationTurnID string `json:"conversation_turn_id,omitempty"`
```

When creating an assistant reply:

1. insert ASSISTANT turn with `delivery_status='PENDING'`;
2. enqueue send job with turn ID.

On successful `bot.Send`:

- set `telegram_message_id`;
- set `delivery_status='SENT'`.

On terminal send failure:

- mark `FAILED`.

This ensures future context only treats actually delivered assistant turns as user-visible.

If implementing this is too invasive for the first patch, at minimum persist assistant replies transactionally before send and add a clear TODO, but the full target should track delivery.

---

# 26. LLM observability migration

Extend `llm_call`.

Suggested columns:

```sql
call_kind TEXT
tool_name TEXT
request_id TEXT
```

Allowed `call_kind`:

- `NATIVE_TOOL`
- `NATIVE_TOOL_RESULT` only if a runtime continuation remains
- `LEGACY_STRUCTURED`
- `LEGACY_UNKNOWN`

Existing historical rows should be `LEGACY_UNKNOWN`; do not falsely backfill them as native.

New production finance calls must be `NATIVE_TOOL`.

After rollout, `LEGACY_STRUCTURED` count should remain zero.

### `gateway.CallMetric`

Add:

- `RequestID`
- `HouseholdID`
- `CallKind`
- `ToolName`

Do not include prompt, raw response, email body, Telegram text, or document data.

### Recorder design

Do not bind the task only through:

```go
WithRecorder("TELEGRAM_NATIVE", ...)
```

because the same client can perform different logical operations.

Prefer per-invocation task metadata.

---

# 27. Admin/operations changes

Update `apps/api/internal/admin/read.go`.

## Overview

Loop over:

```text
INTERACTIVE
CHAT
DEFAULT
BACKGROUND
```

Add CHAT queue health threshold.

## LLM summary

Group/filter by:

- task;
- call_kind;
- tool_name;
- model;
- status.

Add a native-contract signal:

```text
structuredCalls24h
```

or:

```text
nativeComplianceRate
```

Target production after migration:

```text
LEGACY_STRUCTURED = 0
```

## LLM calls endpoint

Return:

- `callKind`
- `toolName`
- `requestId`

Keep content redacted.

## Household overview

Include tool/call kind in recent LLM records.

---

# 28. LLM task naming

Use meaningful task names instead of a single generic `TELEGRAM_NATIVE`.

Suggested values:

- `TELEGRAM_FINANCE_TURN`
- `TELEGRAM_REVIEW_TURN` if separate metrics are useful;
- `BANK_EXTRACTION`
- `DOCUMENT_CLASSIFICATION`
- `PAYSLIP_EXTRACTION`
- `RECEIPT_EXTRACTION`
- `TRANSACTION_SCREENSHOT_EXTRACTION`
- `GENERATE_INSIGHT`

Do not make task naming depend on provider model.

---

# 29. Error and terminal failure behavior

Native-only means the old semantic fallback is gone.

Define explicit failures.

## Telegram model transport/provider failure

- job retries according to queue policy;
- no financial mutation;
- no Structured fallback;
- no deterministic text parser fallback.

After terminal failure:

- mark the chat source event `FAILED`;
- enqueue a user-safe reply:
  - general turn: `Pesan ini belum bisa diproses. Data keuangan tidak diubah. Coba kirim ulang.`
  - review-bound turn: `Balasan ini belum bisa diproses. Review tetap terbuka dan data belum diubah.`

Do not expose provider error text.

Add a `HandleTerminalFailure` path for `PROCESS_TELEGRAM_TEXT`, similar in spirit to document terminal failure handling.

## Invalid model tool call

Treat as gateway/model contract failure.

Do not reinterpret its raw text.

## Ambiguous target

This is not a gateway error.

Execute `ask_clarification`, no mutation, source event is processed normally.

---

# 30. Security requirements

Add explicit tests for all of the following.

1. Model cannot pass another household’s canonical ID.
2. Ephemeral reference from household A cannot resolve in household B.
3. Ephemeral reference from chat A cannot resolve in chat B.
4. Expired refs cannot mutate.
5. Exact Telegram reply cannot resolve a review outside the active household.
6. Category slugs are dynamically allowlisted.
7. Unknown tool args fail.
8. Unknown tools fail.
9. Multiple tool calls fail.
10. Tool call with provider auxiliary prose still uses only the function call.
11. Prompt-injection text inside user messages is treated as data.
12. Tool args cannot alter source status directly.
13. Model cannot mark a review resolved without the corresponding Go state transition.
14. Model cannot create merchant learning without explicit confirmation.
15. Search disclosure remains bounded.

---

# 31. Test plan

## 31.1 Gateway unit tests

Update `apps/worker/internal/gateway/client_test.go`.

Required tests:

- required tool choice set;
- `parallel_tool_calls=false`;
- zero calls rejected;
- multiple calls rejected;
- unknown tool rejected;
- invalid args rejected;
- auxiliary provider prose ignored;
- protocol never silently switches;
- metric contains call kind/tool name/request ID;
- no response content in errors.

Remove/replace tests that validate Structured production behavior.

## 31.2 Static native-only test

Add CI/static test ensuring no runtime Structured callsites.

This is a release gate.

## 31.3 Telegram integration tests

Add end-to-end conversation tests.

### Query follow-up

```text
U: pengeluaran bulan ini berapa?
A: ...
U: yang paling besar apa?
```

Second model request must receive prior assistant/tool context.

### Search reference correction

```text
U: cari transaksi Pamella bulan ini
A: [bounded results]
U: yang kedua sebenarnya kemarin sore
A: asks confirmation
U: iya ubah
A: confirms
```

Assertions:

- no canonical ID from model;
- one pending edit;
- mutation only after confirmation;
- audit entry exists.

### Review without Reply

Create exactly one open review.

```text
Bot previously sent review
U: itu buat belanja rumah
```

No `reply_to_message_id`.

Expected:

- context resolver binds exactly one active review;
- native `resolve_review`;
- transaction/review updated;
- canonical review resolved once.

### Multiple reviews

Create two compatible OPEN reviews.

```text
U: itu buat belanja rumah
```

Expected:

- no mutation;
- clarification response with bounded candidates.

### Exact Reply precedence

When two reviews are open and user replies to one Telegram review message, exact binding wins.

### Pending batch

Natural yes/no goes through required native tools.

### Pending salary choice

Natural text goes through native `resolve_salary_choice`.

### Merchant learning

Natural affirmative works without Reply only when one merchant-learning state is active.

### LLM failure

No Structured fallback and no deterministic transaction parsing occurs.

## 31.4 Review callback regression tests

Ensure inline buttons still:

- acknowledge quickly;
- bind exact review;
- mutate once;
- remove stale buttons/edit original;
- do not call LLM.

## 31.5 Document tests

For payslip/receipt/screenshot:

- fake gateway must fail the test if `Structured` is invoked;
- verify native `Required:true`;
- verify exact tool name;
- verify schema validation;
- existing persistence/reconciliation integration tests remain green.

## 31.6 Insight tests

Ensure Telegram no longer invokes a separate Structured insight generator.

## 31.7 Queue lane tests

- `PROCESS_TELEGRAM_TEXT` -> CHAT;
- callbacks -> INTERACTIVE;
- documents -> BACKGROUND;
- trigger overrides bad producer lane;
- admin returns CHAT metrics.

---

# 32. Recommended code movement

Current `processor.go` and `review.go` are large.

Use this refactor to reduce orchestration coupling without rewriting domain logic.

Suggested layout:

```text
apps/worker/internal/telegram/
  processor.go              // high-level turn lifecycle only
  turn_context.go           // public/private context builder
  turn_store.go             // conversation persistence
  tool_catalog_v2.go        // dynamic native tool schemas
  tool_validate_v2.go       // DTO validation
  tool_executor_v2.go       // dispatch
  transaction_tools.go      // create/batch/correction/pending
  query_tools.go            // spending/cashflow/search/reviews
  review_tools.go           // free-text review tool execution
  review_callbacks.go       // deterministic callback handlers
  reply_renderer.go         // deterministic user-facing messages
  bot.go
```

Do not force the exact filenames if it creates unnecessary churn, but enforce separation between:

- model contract;
- domain execution;
- conversation persistence;
- Telegram transport.

---

# 33. File-by-file modification checklist

## `apps/worker/internal/gateway/client.go`

- redesign invocation metadata;
- hard reject call count != 1;
- native-only runtime public API;
- add reusable strict argument decoder;
- add metric call kind/tool name/request ID/household;
- remove Structured after migration;
- remove NativeToolResult if no production caller remains.

## `apps/worker/internal/gateway/client_test.go`

- native-only contract tests;
- remove legacy structured assumptions.

## `apps/worker/internal/telegram/tool_registry.go`

Either replace or retire.

- no static incomplete catalog;
- dynamic categories;
- state-conditional tools;
- no canonical transaction UUID in conversational correction;
- remove ignored pending IDs.

## `apps/worker/internal/telegram/processor.go`

- replace fallback pipeline;
- remove deterministic extraction;
- remove multi-step native loop;
- build turn context;
- required one native call;
- execute/persist result;
- remove free-text pending/review pre-routing.

## `apps/worker/internal/telegram/assistant.go`

- move DB query logic into result-returning helpers;
- remove Structured Telegram insight;
- deterministic reply renderer only.

## `apps/worker/internal/telegram/review.go`

- preserve deterministic callback mutation;
- remove Structured `extractReview`;
- remove keyword free-text semantic classifiers;
- expose deterministic review mutation helpers to tool executor;
- add contextual active-review resolver.

## `apps/worker/internal/telegram/bot.go`

- add `SendChatAction`;
- add optional conversation turn send metadata.

## `apps/worker/internal/telegram/*_test.go`

- rewrite fake gateways to native-only;
- add conversational state tests.

## `apps/worker/internal/document/processor.go`

- remove Structured from Gateway interface after other extractors migrate.

## `apps/worker/internal/document/payslip.go`

- `extract_payslip` native call.

## `apps/worker/internal/document/receipt.go`

- `extract_receipt` native call.

## `apps/worker/internal/document/screenshot.go`

- `extract_transaction_screenshot` native call.

## `apps/worker/internal/insight/processor.go`

- keep native generator;
- extract reusable service if Telegram needs synchronous access.

## `apps/worker/internal/bankemail/extractor.go`

- mostly keep;
- migrate to common invocation/decoder types.

## `apps/worker/cmd/worker/main.go`

- CHAT lane consumers;
- lower inner Telegram model timeout;
- richer LLM recorder;
- terminal Telegram failure handling;
- typing action wiring.

## `apps/api/internal/telegram/intake.go`

- keep durable intake;
- ordinary text still enqueues `PROCESS_TELEGRAM_TEXT`;
- lane trigger owns routing.

## `apps/api/internal/admin/read.go`

- CHAT metrics;
- call_kind/tool_name fields;
- native compliance.

## `apps/api/internal/llm/client.go`

- delete if unused after `rg`/`go list` verification;
- do not leave legacy Structured gateway as future temptation.

## `.env.example`

Add:

```text
TELEGRAM_CHAT_WORKERS=3
```

Clarify model env vars if task naming changes.

## DB migrations

Add new migrations after current head for:

- CHAT lane;
- conversation turns/refs;
- llm_call observability fields.

---

# 34. Migration sequencing

Because job lane routing affects active workers, use a compatible rollout.

Recommended logical sequence:

### Migration A — additive schema

- allow CHAT lane;
- add CHAT index;
- add conversation tables;
- add LLM observability columns;
- do **not** make old rows invalid.

### Code deploy

Deploy worker code that:

- knows CHAT;
- consumes CHAT;
- still safely handles existing DEFAULT Telegram text jobs.

### Migration B / routing activation

Change `enforce_job_lane()` so new `PROCESS_TELEGRAM_TEXT` becomes CHAT and backfill pending text jobs.

If the deployment system automatically runs every new migration before starting code and cannot stage A/B separately, make sure the new worker is started immediately and document that rollback to an older worker requires remapping CHAT PENDING/RUNNING jobs to DEFAULT.

Do not leave a rollback path where old workers silently ignore CHAT forever.

---

# 35. Observability/rollout gates

Before enabling the new behavior broadly, verify:

1. CHAT p50/p95 queue wait;
2. Telegram model p50/p95;
3. callback p95 remains unaffected;
4. tool validation failure rate;
5. ambiguous clarification rate;
6. source-event failure rate;
7. open review growth;
8. `LEGACY_STRUCTURED` new-call count = 0;
9. cross-household security tests;
10. no increase in duplicate transaction creation.

---

# 36. Commit/PR implementation slices

Do not ship this as one unreviewable mega-diff if avoidable.

Recommended slices:

## Slice 1 — ADRs + contract tests

- ADR-030/031/032;
- status updates to old ADRs;
- add native-only static check;
- add failing/updated tests describing new contract.

No production behavior change yet if possible.

## Slice 2 — Gateway native-only core

- invocation metadata;
- one-call enforcement;
- strict decoder;
- metrics fields in Go;
- update bank/classification/insight callers.

## Slice 3 — Additive DB + CHAT support

- conversation schema;
- llm_call fields;
- CHAT lane support;
- worker CHAT consumers;
- admin lane metrics.

## Slice 4 — Telegram TurnContext

- turn persistence;
- exact and contextual state resolver;
- ephemeral references;
- no mutation changes yet where possible.

## Slice 5 — Tool Catalog V2 + executor

- record/query/correction/control tools;
- result-oriented executor;
- dynamic category schemas.

## Slice 6 — Free-text review migration

- `resolve_review`;
- salary/pending/batch/merchant tools;
- remove keyword/Structured review semantics.

## Slice 7 — Remove Telegram fallback architecture

- `Required:true`;
- remove Structured extraction;
- remove deterministicTextExtraction;
- remove four-step tool-result loop;
- terminal failure behavior.

## Slice 8 — Documents native-only

- payslip;
- receipt;
- screenshot;
- remove document Structured interface.

## Slice 9 — Insight consolidation + legacy cleanup

- remove Telegram Structured insight;
- remove worker Structured API;
- remove unused API legacy LLM client;
- admin native compliance view;
- repository grep clean.

## Slice 10 — final integration/deploy hardening

- full DB integration suite;
- queue concurrency tests;
- Docker builds;
- vet;
- migration rehearsal;
- rollback rehearsal.

---

# 37. Commands Codex should run during implementation

Adapt to repository tooling, but at minimum:

```bash
git status
git rev-parse HEAD
git log -5 --oneline

rg '\.Structured\(' .
rg 'deterministicTextExtraction|extractReview|transferReviewIntent|incomeReviewIntent' apps/worker/internal/telegram
rg 'NativeToolResult|MaxToolCalls' apps/worker

go test ./...
go vet ./...

# Run database/integration tests when TEST_DATABASE_URL is available.
```

After final migration:

```bash
rg '\.Structured\(' apps/worker apps/api/internal --glob '!**/*_test.go'
```

Expected: no runtime finance LLM usage.

Run container/Docker builds used by production as well.

---

# 38. Acceptance conversation

This exact class of interaction is a release gate:

```text
User:
pengeluaran bulan ini berapa?

Richmod:
[authoritative deterministic answer]

User:
yang paling besar apa?

Richmod:
[uses previous tool result/context]

User:
cari yang Pamella

Richmod:
[bounded search with conversational refs]

User:
yang kedua sebenarnya kemarin sore

Richmod:
[uniquely resolves prior result; proposes edit; does not mutate yet]

User:
iya ubah aja

Richmod:
[required native confirm tool; deterministic mutation + audit]

User:
ada yang perlu direview?

Richmod:
[canonical review list]

User:
yang 125 ribu itu buat belanja rumah

Richmod:
[resolves correct review even without Telegram Reply if unique]
```

Required properties:

- every LLM step is a native function call;
- no Structured fallback;
- no regex fallback;
- no arbitrary canonical UUID from model;
- no financial total from model;
- no mutation before confirmation when required;
- exact/auditable canonical state;
- natural free-text follow-up works.

---


# 39. Code reference index

These are the primary baseline references Codex must inspect before editing.

### Telegram

- `apps/worker/internal/telegram/processor.go`
  - `Processor.Process`
  - `recentConversation`
  - `deterministicTextExtraction`
  - `processPendingSalaryChoice`
  - `processPendingEdit`
  - `processPendingBatch`
  - `executeNativeTool`
  - `persistTransaction`
- `apps/worker/internal/telegram/tool_registry.go`
  - `NativeFinanceTools`
  - `ValidateNativeToolCall`
- `apps/worker/internal/telegram/assistant.go`
  - `processAssistantIntent`
  - `replySpending`
  - `replyCashflow`
  - `replySearch`
  - `replyCycleInsight`
- `apps/worker/internal/telegram/review.go`
  - `processBoundReview`
  - `processReviewDetailCallback`
  - `processReviewCategoryCallback`
  - `extractReview`
  - `transferReviewIntent`
  - `incomeReviewIntent`
  - `resolveReview`
  - `resolveTransferReview`
- `apps/worker/internal/telegram/bot.go`

### LLM gateway

- `apps/worker/internal/gateway/client.go`
  - `NativeToolCall`
  - `NativeToolResult`
  - `validateNativeCalls`
  - `Structured`
  - `CallMetric`
- `apps/worker/internal/gateway/client_test.go`

### Native reference implementations

- `apps/worker/internal/bankemail/extractor.go`
- `apps/worker/internal/document/processor.go`
- `apps/worker/internal/insight/processor.go`

### Document Structured callers to migrate

- `apps/worker/internal/document/payslip.go`
- `apps/worker/internal/document/receipt.go`
- `apps/worker/internal/document/screenshot.go`

### Worker / queue

- `apps/worker/cmd/worker/main.go`
- `db/migrations/00016_telegram_interactive_lane.sql`
- `db/migrations/00028_job_lane_lifecycle.sql`
- `db/migrations/00029_review_llm_cycle_contracts.sql`

### Telegram ingress

- `apps/api/internal/telegram/intake.go`

### Admin/operations

- `apps/api/internal/admin/read.go`

### Legacy LLM client

- `apps/api/internal/llm/client.go`

### Review/pending schema

- `db/migrations/00007_telegram_review.sql`
- `db/migrations/00021_universal_review_items.sql`
- `db/migrations/00022_telegram_pending_actions.sql`
- `db/migrations/00023_telegram_pending_batch.sql`
- `db/migrations/00025_salary_pending_choice.sql`

### Existing ADRs

- `docs/adr/ADR-005-cloud-llm-gateway-boundary.md`
- `docs/adr/ADR-008-telegram-review.md`
- `docs/adr/ADR-009-generic-document-intake.md`
- `docs/adr/ADR-010-proposals-before-untrusted-mutation.md`
- `docs/adr/ADR-015-aggregate-only-llm-insights.md`
- `docs/adr/ADR-018-explicit-merchant-learning.md`
- `docs/adr/ADR-020-telegram-finance-assistant.md`
- `docs/adr/ADR-022-telegram-interactive-callback-lane.md`
- `docs/adr/ADR-023-universal-review-items.md`
- `docs/adr/ADR-024-native-finance-tool-calls.md`
- `docs/adr/ADR-025-config-driven-bank-email-ingestion.md`
- `docs/adr/ADR-026-durable-telegram-ingress-and-job-lanes.md`
- `docs/adr/ADR-027-single-protocol-bounded-llm-calls.md`
- `docs/adr/ADR-028-canonical-universal-review-orchestration.md`

---

# 40. Final Codex directive

Treat this work as an architectural correction, not a feature expansion.

The final architecture should be simpler than the current one:

- one LLM contract;
- native tools only;
- one model decision per user turn;
- server-owned state;
- deterministic domain execution;
- persistent conversational context;
- canonical universal review;
- isolated Telegram chat execution;
- observable protocol compliance.

Do not preserve old fallbacks “just in case.” They are the reason the system currently has two semantic architectures.

When a native model call is unavailable or invalid, fail safely and explicitly. Do not silently change how finance intent is interpreted.

The end state should leave the repository with one clear, enforceable LLM/tool architecture that future features can reuse without introducing a second semantic path.

