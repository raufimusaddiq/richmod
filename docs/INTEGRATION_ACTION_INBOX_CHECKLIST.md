# Integration Action Inbox patch checklist

Source: explicit Deploy-1 onboarding patch, 4 September 2026.

- [x] Migration 43 adds generic `integration_action` storage and OPEN dedupe.
- [x] Signed recipient remains the sole household selector.
- [x] Gmail forwarding confirmation detection requires the exact Google sender and subject shape.
- [x] Verification URL requires HTTPS, an exact allowed Google hostname, and `/mail/vf-` path.
- [x] PROVISIONED control delivery creates no financial source, job, review, proposal, or transaction.
- [x] Duplicate confirmation remains one OPEN action.
- [x] OWNER receives URL/code; MEMBER receives redacted metadata; unrelated household receives nothing.
- [x] Resolution is OWNER-only, household-scoped, and audited without URL/code.
- [x] `/actions` is separate from `/reviews` and exposes a pending-count badge.
- [ ] Production migration and deployment observed.
- [ ] Real Gmail confirmation replayed/resent and shown in Integration Action Inbox.
- [ ] External Gmail verification completed by the user.
