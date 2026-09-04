# ADR-033: Cloudflare email ingress and two-deploy Gmail sunset

## Status

Accepted for staged implementation — 4 September 2026.

## Decision

Richmod receives forwarded financial email through a generated, opaque,
household-scoped address at `richmod.link`. Cloudflare stores raw RFC822 bytes
in R2 and sends only delivery metadata through a Queue. A delivery Worker
forwards the raw bytes to `POST /finance/v1/email/inbond`, authenticated with
HMAC-SHA256 over timestamp, recipient, envelope sender, and body SHA-256.

Go validates MIME, authentication evidence, exact household listener matching,
and idempotency, then creates the existing `source_event`, `bank_email_event`,
and `PROCESS_BANK_EMAIL` job. It never calls the LLM or writes ledger state at
the transport boundary. `PROVISIONED` addresses record transport verification
only; `ACTIVE` addresses are the sole financial provider for that household.

Cutover atomically activates the address and disconnects Gmail. Gmail runtime
code and tables are removed only in Deploy 2, after a real production
forwarding transaction has passed. Authentication-service policy remains
configuration-driven and cannot be finalized until a real forwarded `.eml`
has been inspected; no bank or sender is hardcoded. Deploy 1 boot and
PROVISIONED transport do not require `EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS`; an
ACTIVE delivery with that configuration missing is recorded as ignored and
logged without creating financial state.

## Invariants

- recipient, never sender or LLM output, selects household;
- Gmail and Cloudflare cannot both be active for one household;
- duplicate payloads create no second source event or processing job;
- raw email evidence is retained outside Go financial state;
- historical Gmail source records remain after Gmail tables/runtime removal.

## Consequences

Deploy 1 keeps Gmail for migration safety and adds recipient provisioning,
transport verification, and atomic cutover. Deploy 2 deletes Gmail application
integration, jobs, configuration, dependencies, and runtime tables. External
Cloudflare and Google cleanup remains an operational step and is not claimed by
code tests.
