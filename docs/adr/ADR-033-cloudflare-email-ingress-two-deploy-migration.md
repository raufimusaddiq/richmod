# ADR-033: Cloudflare email ingress and two-deploy Gmail sunset

## Status

Accepted; Deploy 2 completed in production — 5 September 2026.

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

While an address is `PROVISIONED`, a deterministic control-email dispatcher may
recognize setup messages such as Gmail forwarding confirmation and create a
generic `integration_action`. These actions appear in a separate Integration
Action Inbox. They never enter `source_event`, `bank_email_event`,
`PROCESS_BANK_EMAIL`, the financial Review Inbox, proposals, the ledger, or the
LLM. Verification URLs are visible only to household owners, require an explicit
Google host/path allowlist, and are opened only by the user's browser.

## Invariants

- recipient, never sender or LLM output, selects household;
- Gmail and Cloudflare cannot both be active for one household;
- duplicate payloads create no second source event or processing job;
- raw email evidence is retained outside Go financial state;
- historical Gmail source records remain after Gmail tables/runtime removal.

## Consequences

Deploy 1 kept Gmail for migration safety and added recipient provisioning,
transport verification, and atomic cutover. Deploy 2 deleted Gmail application
integration, jobs, configuration, dependencies, and runtime tables. External
Cloudflare resources remain an operational dependency and are documented
separately from the application deployment.

After Deploy 2, Cloudflare generic email ingress is Richmod's only application
financial-email ingress. Gmail may remain only as a user's forwarding source;
Richmod no longer authenticates to or consumes Google APIs. The deterministic
Gmail forwarding-confirmation control adapter remains because it processes an
ordinary forwarded setup email, not Gmail API runtime state.

## Operations

The deployed Cloudflare resource names, Worker bindings, Queue/DLQ policy, HMAC
contract, forwarding setup, smoke test, and troubleshooting procedure are
maintained in [`docs/runbooks/cloudflare-email-ingress.md`](../runbooks/cloudflare-email-ingress.md).
