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
Incoming, pocket, and RDN events are ignored by the spending ledger; unknown
outgoing transfers require review.
