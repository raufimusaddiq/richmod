# P0 YAGNI cleanup audit

Baseline: `705b2d023f6930293fc78d4b9521fb1b95d6bd06`. Production worker inspected
at `2026-09-30`; product behavior and database schema unchanged.

## Disposition

- **Telegram legacy text: retained.** Production routes normal
  `PROCESS_TELEGRAM_TEXT` to `ProcessAgent`; `Process` is still used by direct
  integration tests that exercise legacy text-bound replies. Those tests expose
  real behavior not fully covered by the active agent path (bound dates, missing
  amounts, and transfer replies). Deletion would change behavior; no P0-A code
  removed.
- **`review.go`: mechanically split.** Moved workflow functions into
  `review_binding.go`, `review_bank.go`, `review_payslip.go`,
  `review_transfer.go`, `review_duplicate.go`, `review_email.go`, and
  `review_native.go`. Compared all 80 original function bodies against the
  baseline: all present and byte-identical. `review.go` fell from 2,999 to
  1,033 lines. Telegram tests and build/vet pass.
- **Document interpretation: retained.** Running production worker has
  `RICHMOD_DOCUMENT_INTERPRETATION` empty; deployed data has no shadow stages.
  State is **LEGACY**. But ADR-037 explicitly preserves the staged rollout
  until typed extraction and rollout gates complete. Runtime inactivity alone
  does not supersede that current architecture contract. No rollout code/wiring
  removed.
- **Bank-email shadow: removed.** The sole production job writer is
  `apps/api/internal/emailingress/service.go`, hard-coding `shadow=false`;
  worker is the sole consumer. Production’s 142 observed `PROCESS_BANK_EMAIL`
  jobs were `false`, and `bank_email_extraction` had zero non-null shadow
  results. Removed only the payload field and shadow execution/comparison path;
  historical database columns remain. Existing `shadow=true` replay payloads
  are outside active production writers and no operator path was found.

## Parity and verification

Bank normal processing now takes the same non-shadow path unconditionally: same
extractor, Go policy, category decision, validation, persistence, reconciliation,
confirmation/review, and errors. The migration removes no call and changes no
SQL canonical semantics. Existing bank-email tests remain as regression coverage.

No product behavior changes. No model calls added or removed on the active path.
No schema changes. Telegram legacy orchestration and document rollout paths
intentionally deferred.
