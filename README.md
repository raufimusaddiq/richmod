# Richmod

Richmod helps a household understand where its money went without turning
personal finance into a second job.

It gathers income and expense evidence from forwarded financial emails,
Telegram messages, and uploaded documents; turns clear facts into ledger
entries; and sends anything uncertain to a human for review. The result is a
shared financial record that stays useful without asking an LLM to guess.

Richmod is self-hosted. PostgreSQL is the source of truth, Go owns every
financial state change, and deterministic features keep working when the LLM
gateway is unavailable.

## What you can do

- Track household income and expenses in one canonical ledger.
- Forward financial notifications through a private Richmod email address.
- Record or review transactions from Telegram.
- Upload receipts, payslips, invoices, transfer proofs, and other financial
  documents.
- Resolve incomplete or ambiguous information from the same Inbox on web or
  Telegram.
- Explore deterministic spending, category, merchant, member, and salary-cycle
  analytics.
- Keep raw evidence and audit history attached to the decisions they support.

The web app is organized around Overview, Transactions, Analytics, Inbox,
Documents, Household, and Settings. Inbox keeps financial reviews and
integration actions in separate views so setup work never masquerades as a
transaction problem.

## How Richmod treats financial data

Richmod is intentionally conservative:

- PostgreSQL holds canonical financial state.
- Money is never represented with floating-point values.
- Every mutation is household-scoped and auditable.
- Source evidence is preserved; deduplication links evidence instead of
  deleting it.
- LLM output is treated as untrusted input and validated by Go before anything
  reaches the ledger.
- Ambiguity becomes a review item. It is not silently guessed.

Merchant learning follows the same rule. Choosing a category once changes only
that transaction. A permanent merchant rule is created only after a separate,
explicit confirmation.

## Ways data enters Richmod

### Financial email

Each household receives an opaque address shaped like
`h_<32hex>@richmod.link`. That signed recipient selects the household; sender,
subject, body, institution name, and LLM output never do.

Cloudflare stores the original RFC822 message in R2, then sends it to
`POST /finance/v1/email/inbond` with HMAC and content-hash verification.
Forwarding setup messages appear as integration actions and never enter the
financial pipeline.

### Telegram

Household members link Telegram with an expiring, single-use invitation. Text,
images, documents, and review replies are processed asynchronously. Review
replies remain bound to the transaction they were sent for rather than relying
on the LLM to infer context.

### Documents

Receipts, payslips, screenshots, transfer proofs, invoices, and transaction
histories share one evidence pipeline. Extraction may use the LLM gateway, but
Go validates the result and decides whether it becomes a proposal, enriches an
existing transaction, or needs review.

## Current production status

Generic Cloudflare email ingress is active. On September 5, 2026, a real
forwarded Jago notification completed the ACTIVE path through bank-email
processing and ledger confirmation. The Deploy 2 code removes Gmail OAuth,
Pub/Sub, history/watch jobs, configuration, and runtime tables; Gmail remains
only as an optional forwarding source outside Richmod. Real second-sender
acceptance and another off-host backup restore exercise remain open.

For current product scope and verification evidence, see
[`docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md`](docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md)
and [`docs/MVP_COMPLETION_CHECKLIST.md`](docs/MVP_COMPLETION_CHECKLIST.md).

## Run locally

1. Copy `.env.example` to `.env` and replace every placeholder.
2. Run `docker compose up --build`.
3. Open `http://localhost:8080/healthz` and
   `http://localhost:8080/readyz`.

Compose binds application ports to loopback. Production uses host-managed Caddy
for TLS and receives secrets from the existing host environment; secrets do not
belong in the repository.

## Create the first owner

Bootstrap can run exactly once. Pass the password through standard input so it
does not land in shell history:

```text
printf '%s\n' 'use-a-unique-12-plus-character-password' | docker compose exec -T api /bootstrap --email owner@example.com --name 'Owner Name' --household 'My Household'
```

This creates the first OWNER, household, and Indonesian category seeds in one
transaction. Web sessions expire after 24 hours of inactivity and renew on each
authenticated request.

## Shipping changes

Pull requests and pushes to `main` run secret scanning, Go tests and vet,
database-backed integration tests, frontend tests, the Next.js production
build, Compose validation, and production image builds.

Successful `main` builds publish immutable images to GHCR. Production deploys
are manual and approval-gated; the server pulls those images, runs migrations,
and restarts services without building locally. See
[`docs/runbooks/production-deployment.md`](docs/runbooks/production-deployment.md).
