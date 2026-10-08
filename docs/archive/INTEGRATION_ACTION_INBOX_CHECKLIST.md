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
- [x] One `/inbox` surface exposes separate Transaction and Integration Action views; storage, APIs, and resolution semantics remain isolated.
- [x] Production migration and deployment observed on 4 September 2026.
- [x] Real Gmail confirmation resent, deterministically recognized, and shown in Integration Action Inbox.
- [x] External Gmail verification completed by the user; the household-scoped action was marked `RESOLVED` and audited.
