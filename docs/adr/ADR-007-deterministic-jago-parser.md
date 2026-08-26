# ADR-007: Deterministic Jago parser with LLM fallback

## Status

Accepted.

## Decision

Known Jago email templates are classified and parsed deterministically. An
unrecognized but trusted template is retained and may use structured LLM fallback
before Review Inbox handling; it is never blindly posted.

`jago-v1` recognizes merchant/QR payments, debit-card transactions, outgoing
transfers, incoming money, pocket movements, and RDN movements by subject family.
It reads semantic HTML labels, parses whole IDR amounts and Jakarta timestamps,
and requires the configured mailbox/domain plus passing DKIM and DMARC metadata.
Incoming, pocket, and RDN events are ignored by the spending ledger. Outgoing
transfers use a neutral `UNCLASSIFIED` proposal/transaction while unresolved;
known-account policy or an explicitly bound web/Telegram decision performs the
final deterministic classification.

Gmail authorization uses the minimum read-only scope. OAuth state is single-use
and expires after ten minutes; the callback verifies the authorized Gmail profile
matches the configured mailbox. Refresh tokens are encrypted with AES-256-GCM and
household-bound associated data before PostgreSQL storage.

Pub/Sub push requests require a Google-signed OIDC bearer token with the exact
configured audience and verified service-account email. Notifications are deduped
by Pub/Sub message ID, preserved as evidence, and queued before acknowledgement.
Both standard wrapped Pub/Sub JSON and authenticated unwrapped payload delivery
are accepted. Unwrapped messages use `x-goog-pubsub-message-id` when metadata is
enabled; otherwise a SHA-256 payload identity provides deterministic replay
deduplication.
Notification evidence uses the `SYSTEM` source type; fetched Gmail messages use
`BANK_EMAIL` and retain the Gmail message ID for idempotency. The worker refreshes
the encrypted OAuth credential, walks `history.list`, fetches only added messages,
and only parses the exact trusted sender. Learned merchant aliases are applied
first; otherwise the cloud gateway may suggest an allowed category, with Go
validating the slug and confidence before any automatic confirmation.
