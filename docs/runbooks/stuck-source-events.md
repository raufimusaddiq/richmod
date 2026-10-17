# Stuck source events: unfinished but no review path

## What it detects

A source event whose `processing_status` is `NEEDS_REVIEW` (the ledger shows it
as unfinished) but which no active review item can resolve. "Reachable" means an
`OPEN` or `PENDING_SEND` `review_item` that points at the event itself, at one
of its transactions (via `transaction_evidence`), proposals, document, wealth
observation, provider-email observations, or an album page's document. Superseded
evidence (`link_status` other than `ACTIVE`) does not count as a link. Such a record can only be left by
Reject or by direct SQL, which is the failure mode of ticket #353. Ticket #356
tracks the first batch found in production.

The query is read-only and household-scoped on demand. It does not repair
anything: a repair changes canonical state and needs owner approval.

## Run it

From Go: `operations.StuckSourceEvents(ctx, pool, householdID)`; pass `""` to scan
every household. The query below is the **all-household scan**: its single
`NULL::uuid` is the household filter, so to scope it from psql replace that one
occurrence with `'<household uuid>'::uuid`.

```sql
SELECT se.id, se.household_id, se.source_type, se.received_at,
  EXISTS (SELECT 1 FROM review_item ri WHERE ri.household_id = se.household_id
          AND ri.status IN ('RESOLVED','CANCELLED','EXPIRED') AND ri.source_event_id = se.id) AS had_source_review,
  (SELECT count(*) FROM transaction_evidence te JOIN transaction t ON t.id = te.transaction_id
    WHERE te.source_event_id = se.id AND t.status = 'NEEDS_REVIEW'
      AND COALESCE(te.metadata_json->>'link_status','ACTIVE') = 'ACTIVE') AS needs_review_transactions,
  (SELECT count(*) FROM transaction_proposal p
    WHERE p.source_event_id = se.id AND p.proposal_status = 'NEEDS_REVIEW') AS needs_review_proposals,
  EXISTS (SELECT 1 FROM document d WHERE d.source_event_id = se.id
          OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id)) AS has_document
FROM source_event se
WHERE se.processing_status = 'NEEDS_REVIEW'
  AND se.household_id = COALESCE(NULL::uuid, se.household_id)
  AND NOT EXISTS (
    SELECT 1 FROM review_item ri
    WHERE ri.household_id = se.household_id AND ri.status IN ('OPEN','PENDING_SEND')
      AND (ri.source_event_id = se.id
        OR ri.transaction_id IN (SELECT te.transaction_id FROM transaction_evidence te WHERE te.source_event_id = se.id
                                 AND COALESCE(te.metadata_json->>'link_status','ACTIVE') = 'ACTIVE')
        OR ri.proposal_id IN (SELECT p.id FROM transaction_proposal p WHERE p.source_event_id = se.id)
        OR ri.document_id IN (SELECT d.id FROM document d WHERE d.source_event_id = se.id
                              OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
        OR ri.wealth_observation_id IN (SELECT w.id FROM wealth_observation w JOIN document d ON d.id = w.document_id
                                        WHERE d.source_event_id = se.id
                                        OR EXISTS (SELECT 1 FROM document_page dp WHERE dp.document_id = d.id AND dp.source_event_id = se.id))
        OR ri.financial_email_observation_id IN (SELECT f.id FROM financial_email_observation f WHERE f.source_event_id = se.id)))
ORDER BY se.received_at, se.id
```

## Reading the result

| Column | Meaning |
| --- | --- |
| `had_source_review` | The event once had a review item that is now resolved, cancelled or expired: the status was left behind. |
| `needs_review_transactions` | Linked transactions still `NEEDS_REVIEW` with no active item: reopen candidates. |
| `needs_review_proposals` | Linked proposals still `NEEDS_REVIEW` with no active item. |
| `has_document` | The event has a document; check its status before choosing a repair. |

Cost: `source_event.processing_status` is not indexed and the reachability test
is a correlated `NOT EXISTS`, so the query scans `source_event`. That is fine
for an operator or post-deploy check at household scale; do not wire it into a
per-request path.

A healthy system returns zero rows. Any row is a defect to triage, not noise;
run the query after deploys and when the Review Inbox count looks lower than the
ledger's unfinished count.

## Ticket #356 repair (migration 00084)

Migration `00084_settle_stuck_source_events.sql` settles 15 of the 17 events
found in production. It names no row: each group is found by its state
signature among events received before 2026-10-07 (later events are never
touched and stay visible to the detection query), and the migration raises
unless the group has exactly its expected size (A 6, B 3, C 1, D 5) or is empty.
Per event it writes one `audit_log` row (`REPAIR_STUCK_SOURCE_EVENT`, with the
prior state) and never creates, changes or deletes a transaction, evidence,
proposal, job or review row.

| Group | Signature | Settlement |
| --- | --- | --- |
| A | `TELEGRAM_TEXT`, `NEEDS_REVIEW`, no parser, nothing linked | `PROCESSED` |
| B | `BANK_EMAIL`, extraction `INVALID`/`NEEDS_REVIEW`, nothing linked | `FAILED` plus the dismissable failed-source item (same text as `RecordFailedSourceAction`); not re-extracted |
| C | document wealth observation `APPLIED`, item resolved | `PROCESSED` |
| D | document item resolved `IGNORE`, document `EXTRACTED` | source `IGNORED` (the document stays `EXTRACTED`) |

Groups A and C also stamp `parser_name = 'repair-00084'` (the prior value is in
the audit row). `Down` restores each event from its audit row only while it is
still in the state Up left it in, including that stamp, so an event a worker
processed since is never flipped back; an event the household has acted on since is left alone, and a later
Up counts it as already settled.

**All-or-nothing, and what that means between deploys.** If some groups match
and others match nothing, the migration raises and the deploy fails loudly. That
is the expected result if any of these events was already settled by hand
before the deploy (for example group D dismissed in the Inbox, or the two
transfer proofs below if they somehow matched a group). Settle by hand only
after the deploy, or re-run the detection query first and expect the sizes
above.

### After deploying it

The detection query above must return exactly two rows: the transfer proofs
below. Any other row is a defect to triage.

### The two transfer-proof events (settled by hand, not by the migration)

Two `TELEGRAM_IMAGE` events (`TRANSFER_PROOF` documents, `EXTRACTED`) carry one
stale `NEEDS_REVIEW` proposal each and no evidence. A one-off manual statement
(audit action `MANUAL_UNLINK_EVIDENCE`, SYSTEM, 2026-10-07 06:44, reason "PR300
screenshot time guard") unlinked both from the same CONFIRMED expense after the
screenshots had been matched to it, and left the proposals `NEEDS_REVIEW` with
no review item.

They are not in the migration on purpose. Reprocessing queues
`PROCESS_TRANSACTION_SCREENSHOT`; if the matcher no longer finds the original
transaction (amount drift, or outside the 72-hour window) the auto-confirm path
(`confirmScreenshotRow`) writes a second CONFIRMED row for the same real
transfer. The original unlink was the time guard rejecting at validation, so
that outcome is plausible. Do not reprocess these two until a code fix makes a
reprocess prefer the superseded proposal's `matched_transaction_id` over
auto-confirm (tracked in #363).

Settle them by hand with owner approval, one source at a time (`:source` is the
source event id; `:matched` is the transaction in the proposal's
`matched_transaction_id`):

1. **Before:** record the household's CONFIRMED transaction count and the
   matched transaction's evidence:

   ```sql
   SELECT count(*) FROM transaction WHERE household_id = :household AND status = 'CONFIRMED';
   SELECT te.source_event_id, te.evidence_type, COALESCE(te.metadata_json->>'link_status','ACTIVE')
   FROM transaction_evidence te WHERE te.transaction_id = :matched;
   SELECT p.id, p.proposal_status, p.metadata_json->>'matched_transaction_id' FROM transaction_proposal p WHERE p.source_event_id = :source;
   ```

2. **Settle**, in one reviewed transaction with an `audit_log` row per event:
   re-link the screenshot as evidence of `:matched` (`transaction_evidence`,
   `TRANSACTION_SCREENSHOT`, the same shape `LINK_SCREENSHOT_EVIDENCE` writes),
   set the proposal `MERGED`, and set the source event `PROCESSED`. Do not
   queue a job and do not insert a transaction. If the owner decides the
   screenshots are not wanted as evidence, set the proposal `REJECTED` and the
   source `IGNORED` instead.
3. **After:** re-run the three queries. The CONFIRMED count must be unchanged,
   the matched transaction must show the new evidence (or none, for the reject
   case), and no second transaction may exist for the same amount and time. The
   detection query must now return zero rows.
