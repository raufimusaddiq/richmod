# ADR-008: Telegram human-in-the-loop review

## Status

Superseded by ADR-031.

## Decision

Telegram is finance-only. Review replies bind to the stored Telegram message ID,
not inferred LLM conversation context.

Telegram webhook intake authenticates `X-Telegram-Bot-Api-Secret-Token`, accepts
only private text messages from an active numeric Telegram user mapping, preserves
the source update as evidence, and atomically enqueues PostgreSQL-backed work.
Duplicate update IDs are acknowledged without creating duplicate jobs. LLM work
never runs synchronously in the webhook request.

The worker treats the message as untrusted data, requests strict structured output,
and validates type, whole-IDR amount, Asia/Jakarta time, category membership, and
confidence before mutation. Every valid extraction first creates a proposal. Only
unambiguous high-confidence output can be confirmed automatically; all other valid
transactions enter `NEEDS_REVIEW`. Bot replies are separate queued jobs.
