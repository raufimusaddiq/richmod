# ADR-018: Explicit merchant category learning

## Status

Accepted.

## Context

A merchant may represent several household purposes. Treating one category
correction as a permanent rule can silently misclassify later transactions,
especially for marketplaces, ride-hailing services, and supermarkets.

## Decision

Confirming a reviewed transaction changes only that transaction by default. It
does not create or enable a `merchant_alias`.

The web confirmation form exposes an unchecked “remember this merchant” choice.
Telegram confirms the transaction first and, when the transaction has both a
merchant and category, sends a separately bound yes/no follow-up. Only an
explicit affirmative creates or enables the alias with `auto_apply=true` and
`created_from_user_confirmation=true`. Ambiguous Telegram replies keep the
follow-up open and do not create a rule.

Merchant aliases remain household-scoped and auditable. OWNER users can list
rules and switch `auto_apply` off without deleting the rule or its history.
Deterministic ingestion applies only enabled aliases; a disabled rule therefore
stops affecting future proposals.

When an `UNKNOWN_MERCHANT` Telegram review receives a merchant name, Richmod
also checks an enabled, explicitly confirmed alias using household-scoped,
case-insensitive, whitespace-normalized matching. A match reuses its stored
merchant and category and completes the review without asking for the same
category again. Missing, disabled, or cross-household aliases still require an
explicit category choice.

## Consequences

- One-off categorization cannot silently change future transactions.
- Rule creation requires an additional deliberate user action on both channels.
- Existing ingestion behavior remains deterministic and available when the LLM
  gateway is unavailable.
- Rule history and financial evidence are preserved.
