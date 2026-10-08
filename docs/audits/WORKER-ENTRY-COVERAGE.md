# Worker entry-point test coverage

**Date:** 2026-10-08, on `a9bad8e` (deployed). Pre-`v0.beta` check.
**Question:** for every job the worker runs, does a test call the same entry
function the worker calls? `TELEGRAM-REPLY-PATH-AUDIT.md` found tests that passed
on an entry production never used; this checks the rest of the worker for that
class of gap.

| Job | Worker entry | Tests calling the entry |
| --- | --- | --- |
| `PROCESS_TELEGRAM_TEXT` | `Processor.ProcessAgent` | 12 integration files |
| `PROCESS_TELEGRAM_CALLBACK` | `Processor.Process` | 12 integration files |
| `FETCH_TELEGRAM_IMAGE` | `ImageProcessor.Process` | 1 integration file |
| `SEND_` / `EDIT_TELEGRAM_MESSAGE` | `BindReviewMessage`, `ReviewProjectionOpen`, `BindEvidenceMessage` | 6 integration files |
| `PROCESS_FINANCIAL_EMAIL` / `_PREVIEW` | `financialemail.Processor.Process` / `ProcessPreview` | 1 integration file each |
| `PROCESS_DOCUMENT` | `document.Processor.Process` | 1 integration file |
| `PROCESS_PAYSLIP` | `ProcessPayslip` | 1 integration file |
| `GENERATE_INSIGHT` | `insight.Processor.Process` | 1 integration file |
| `GENERATE_CYCLE_RESIDUAL_REVIEW` | `residual.Processor.Generate` | 2 integration files |
| `COMPLETE_BANK_REVIEW` | `bankemail.Processor.Complete` | **none before this note**; added `TestCompleteBankReviewRecordsSuppliedFactsOnce` |
| `PROCESS_BANK_EMAIL` | `bankemail.Processor.Process` | **none** |
| `PROCESS_RECEIPT` | `document.Processor.ProcessReceipt` | **none** |
| `PROCESS_TRANSACTION_SCREENSHOT` | `document.Processor.ProcessScreenshot` | **none** |
| terminal failure steps | `TerminalTextFailureTx`, `TerminalCallbackFailureTx`, bank `TerminalFailureTx`, document `HandleTerminalFailure` | 1–2 integration files each |

## Reading

Unlike the Telegram reply lane, no job here is tested through an entry
production does not use. The three remaining gaps are narrower: their tests call
the real steps inside the entry (`applyCategoryDecision`, `persistReceipt`,
`persistScreenshot`, `reviewIncompleteExtraction`, …), so the policy and
persistence are covered, but the entry's own glue is not: loading the source or
document, the "already processed" guards, the extraction call and the order of
steps. Each needs a scripted extraction gateway (and, for documents, a stored
image) to drive end to end.

`COMPLETE_BANK_REVIEW` had the same gap and is now covered, because the Telegram
bank-fact fix (`TELEGRAM-REPLY-PATH-AUDIT.md`, F2) queues it.

## Follow-up (not blocking `v0.beta`)

Add one end-to-end test per remaining entry, driving it with a scripted
extraction gateway: `bankemail.Process` (main production source),
`ProcessReceipt`, `ProcessScreenshot`.
