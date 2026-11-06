# ADR-059: Super Admin household provisioning

## Status

Accepted — 5 November 2026. Extends ADR-058 (multi-household tenancy) and
ADR-021 (multi-user dashboard auth).

## Context

ADR-058 made one deployment safe for many households and added
`/bootstrap --new-household`, which needs a shell on the production host.
Creating tenants should not require server access, and the operator should not
have to choose or transmit an owner's password.

## Decision

1. **Super Admin creates households in the console.**
   `POST /api/v1/admin/households` (Super Admin only, same-origin, behind the
   session) takes `householdName`, `ownerName`, `ownerEmail` and an optional
   positive `telegramUserId`. Unknown fields are rejected.
2. **One provisioning path.** The console and the bootstrap command both call
   `internal/provision.CreateHousehold`: household, a **new** `OWNER` user, the
   optional Telegram identity and the default categories in one serialized
   transaction, audited as `CREATE_HOUSEHOLD` in the new household. An existing
   email (including the Super Admin's own) returns `409 USER_ALREADY_EXISTS`;
   no user is ever attached to or moved between households (ADR-035).
3. **Identity bindings are 1:1 and permanent.** A Telegram user ID belongs to
   exactly one user, a user has at most one Telegram ID, and a user belongs to
   exactly one household, whether the membership is active or not. Migration
   `00088` enforces it: `household_member(user_id)` unique,
   `telegram_identity(user_id)` unique (the Telegram ID is already the primary
   key), and `telegram_identity(household_id, user_id)` must reference that
   user's own membership. Removal only deactivates; nothing is ever rebound or
   moved. So a Telegram ID already bound to anyone (including a removed member)
   returns `409 TELEGRAM_ID_ALREADY_LINKED`, and the Telegram link-invite flow
   applies the same rule (`provision.BindTelegramIdentity`), which may only
   reactivate the same user's own identity. This amends ADR-035, which allowed
   a user with an inactive membership to join another household.
   The Telegram ID is optional at creation: an owner without one links later
   from their own row on the household page, through the same link invite
   members use (`POST /api/v1/household/members/{id}/telegram-invite`), which
   accepts any active member without an active identity, owner included.
4. **Owners enroll by invite; no password passes through the API.** The console
   owner gets an unusable random password with `password_initialized_at` unset
   (login refuses such a user) and a single-use, 24-hour dashboard invite
   (ADR-021: only the token's SHA-256 is stored). The response shows the
   `/invite#…` link once; the Super Admin hands it to the owner, who sets their
   own password.
5. **Reissue while not enrolled.** Owners can invite members, but nobody can
   invite an owner, so `POST /api/v1/admin/households/{id}/owner-invite`
   replaces the owner's pending invite until the owner enrolls; afterwards it
   returns `409 OWNER_ALREADY_ENROLLED`.
6. **Creating is not access.** The Super Admin does not become a member of the
   household it creates and still cannot read its financial data (ADR-021).
   Both actions are recorded in `platform_audit_log` (`ADMIN_CREATE_HOUSEHOLD`,
   `ADMIN_REISSUE_OWNER_INVITE`) with the request ID; the new household's own
   `audit_log` names the Super Admin as the creator.
7. **The owner link is a bearer credential.** Until the owner enrolls, whoever
   holds the current invite link can become that owner, and the Super Admin is
   the one who receives it, at creation and on every reissue. This is inherent
   to console provisioning without outbound email: Super Admin is trusted to
   hand the link only to the owner. Every issuance is platform-audited, only the
   newest link is valid, and once the owner enrolls no link can be issued
   again. Owners who enroll promptly close the window.

## Consequences

- Tenant creation needs only a Super Admin session; shell access stays an
  operator fallback (`/bootstrap --new-household`, where the operator sets the
  password directly).
- A lost invite link cannot be recovered, only reissued.
- The household overview shows whether each member has enrolled.
