# ADR-017: Telegram member self-link invitations

## Status

Accepted.

## Decision

An OWNER creates a household MEMBER before Telegram linking. Richmod returns a
15-minute, single-use deep link containing a cryptographically random token and
stores only its SHA-256 hash. Invitations are scoped to the household and member,
audited, revocable, and transition through `PENDING`, `CONSUMED`, `REVOKED`, or
`EXPIRED`.

The Telegram webhook recognizes `/start <token>` before normal identity
authorization. It locks and validates the invitation, takes the numeric sender ID
from Telegram, creates the `telegram_identity`, consumes the invitation, records
the audit event, and queues a confirmation message in one PostgreSQL transaction.
Usernames and owner-entered numeric IDs are never accepted as identity proof.

Member deactivation preserves the user, membership, transactions, evidence, and
audit history while disabling the membership, Telegram identity, and pending
invitations. Authentication loads active memberships only.

## Consequences

New members have no owner-known password: an unrecoverable random Argon2id secret
is stored until a separate password enrollment flow is designed. They can use the
finance-scoped Telegram flow immediately after linking. A later web-login
enrollment must introduce its own short-lived proof rather than exposing or
resetting this generated secret.
