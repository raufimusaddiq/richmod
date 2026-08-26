# ADR-021: Multi-user dashboard authentication

## Decision

Household membership remains `OWNER` or `MEMBER`. Dashboard enrollment uses a single-use, 24-hour invite whose SHA-256 token hash is stored in PostgreSQL; the token is carried in the URL fragment and exchanged over HTTPS. Acceptance sets a password and creates the normal sliding 24-hour HTTP-only session. New members cannot log in until enrollment.

The platform supports a separate `is_super_admin` user flag. It grants metadata/admin-console access only and does not bypass household-scoped financial authorization. Raufi Musaddiq remains the household OWNER and is promoted through the idempotent bootstrap command.

## Security

Invite tokens are never persisted or logged in plaintext. Invites are consumed transactionally, and deactivation revokes sessions and pending access in follow-up work. Existing owner accounts are initialized by migration; existing members must enroll.
