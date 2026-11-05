# ADR-058: Multi-household tenancy hardening

## Status

Accepted — 5 November 2026. Extends ADR-035 (one active household per user)
and ADR-003 (PostgreSQL job queue).

## Context

`household_id` has always been the tenant key, and an audit of every lane (Web
API, Review Inbox, Telegram, bank and financial email, documents, analytics,
push, worker jobs) found no path that lets one household read or change
another's financial data. Isolation was, however, purely application-level:
92 of 93 cross-table foreign keys were single-column, so the database would
accept a household A row pointing at household B's category, transaction or
review. The audit also found that the deployment could not actually host a
second household, and several problems that only appear once households share
one deployment:

- the bootstrap command was the only way to create a household and refused to
  run once any owner existed;
- one household's settled salary cycle made the residual catch-up `INSERT`
  fail for every household;
- the owner's operational status counted every household's jobs;
- one FIFO queue per lane let one household's backlog delay all others, and
  non-Telegram model calls were recorded without a household;
- a member removed while work was queued could still receive cards and
  replies; removed members kept dashboard invites and sessions, and a
  platform-disabled user kept Telegram access;
- sender authentication trusted any `Authentication-Results` header in the
  message, including one written by the sender.

## Decision

1. **The database enforces same-household references.** Every foreign key
   between two household-scoped tables is backed by a composite foreign key
   `(household_id, <ref>) → parent(household_id, id)` (migration `00087`),
   validated against existing rows. A cross-household reference already in the
   data fails the migration explicitly; it is never repaired silently.
   Telemetry tables (`product_telemetry_event`, `judgment_turn_telemetry`,
   `intelligence_phase_telemetry`) are excluded: their rows are written by
   triggers inside financial transactions and must not be able to abort them.
   Application-level `household_id` filters stay; the database is the backstop.
2. **Jobs have an owning household.** `job.household_id` is derived once at
   enqueue by the `job_assign_household` trigger from the payload's anchor row
   (`household_id`, `source_event_id`, `document_id`, `review_request_id`,
   `review_id`, `preview_id`, `insight_id`, or the private Telegram `chat_id`).
   Enqueue sites are unchanged; platform jobs keep `NULL`. The payload stays the
   dispatch contract.
3. **Lanes are shared fairly.** Only each household's oldest due job competes,
   and a claim takes the one whose household's latest job in that lane started
   longest ago, so one household's backlog cannot starve another's. Platform
   jobs (no household) rotate as one more group. Within one household the order stays
   `run_after, created_at`, which keeps one person's Telegram messages in order.
4. **Model calls are attributed.** The worker attributes every job's model
   calls to `job.household_id`, so `llm_call` and phase telemetry carry the
   household for documents, email, insights and residual work too.
5. **Owner status is household-scoped.** `GET /api/v1/operations/status`
   counts only the owner's household's jobs. The platform-wide queue belongs to
   Super Admin (`GET /api/v1/admin/overview`). The worker heartbeat stays shared.
6. **Access is re-checked at the moment of use.** A Telegram user is served
   only while their identity is active, their user is active, and they are an
   active member of that household. Intake checks this at capture; the worker
   checks it again before it processes a queued message, and before it sends or
   edits a message. Removing a member also revokes pending dashboard invites
   and live sessions, and an invite only enrolls a still-active member.
   Disabling a user revokes sessions and stops Telegram access without
   deactivating the identity, so re-enabling restores it.
7. **Additional households are created by the bootstrap command.**
   `/bootstrap --new-household` creates a household with a **new** owner user,
   the optional Telegram identity and the default categories in one
   transaction, audited as `CREATE_HOUSEHOLD`. It refuses an existing email or
   a Telegram ID that is already bound to a user (ADR-059 makes these bindings
   1:1 and permanent), so no user ever moves household implicitly
   (ADR-035). The first-run bootstrap is unchanged. ADR-059 adds the same
   provisioning to the Super Admin console.
8. **Only the receiving MTA's authentication results count.** Ingress reads
   only the topmost `Authentication-Results` header, requires a trusted
   authserv-id, parses `dkim=pass` and `dmarc=pass` as method results (comments
   ignored), and requires a DMARC `header.from`, when present, to equal the
   routed From domain. `ARC-Authentication-Results` is never trusted: without
   validating the ARC-Seal chain (RFC 8617) it is sender-writable. This relies
   on the trusted receiver (Cloudflare, `mx.cloudflare.net`) prepending its own
   `Authentication-Results` to every message.

## Consequences

- A future query that forgets `household_id` can still read another
  household's row, but no write can link households together.
- Tests that deliberately planted cross-household links to probe read-side
  defense now assert that the database rejects the link.
- Telegram replies are never sent to a chat that is not an active member; a
  turn from a removed member is settled `IGNORED` silently.
- The residual catch-up treats a `SUCCEEDED` job as owning its cycle, matching
  the unique index; one pass shares its batch across households.
- Migration `00087` adds composite unique keys and foreign keys; rollback drops
  them and `job.household_id`.
