# P0 YAGNI cleanup audit

Baseline: `705b2d023f6930293fc78d4b9521fb1b95d6bd06`. Production worker inspected
at `2026-09-30`; product behavior and database schema unchanged.

## Disposition

- **Telegram legacy text: retained.** `cmd/worker/main.go` routes normal
  `PROCESS_TELEGRAM_TEXT` to `ProcessAgent`, callbacks to `Process`. However,
  `ProcessAgent` delegates callbacks to `Process`; existing package integration
  tests also invoke `Process` with `TELEGRAM_TEXT`. No complete proof excludes
  replay/admin/internal direct invocation. P0-A deletion and entrypoint split
  are therefore unsafe.
- **`review.go`: not split in this pass.** The proposed ownership boundaries
  overlap shared orchestration and helpers. A move-only change still needs a
  dedicated mechanical diff and parity gate; no behavioral cleanup bundled.
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
No schema changes. Telegram and document rollout paths intentionally deferred.
