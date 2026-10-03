# CEU-07 — Corpus, metrics, and freeze status

**Status:** corpus-complete; production observation **open**. CEU is **not frozen**
until the owner-household observation below is accepted.
**Date:** 2026-10-03
**ADR:** [ADR-050](../adr/ADR-050-conversational-evidence-understanding.md)
**Plan:** [`plans/ceu-execution.md`](../plans/ceu-execution.md)
**Audit:** [CEU-00](CEU-00-architecture-audit.md)

This follows the SAVR-09 pattern: the deterministic corpus is green and measured,
the metrics exist, and the product observation is the owner household's normal use.
No synthetic production data is created, and a rare family may be recorded as
`PRODUCTION_UNOBSERVED` with corpus evidence.

## Delivery

| Slice | PR | Outcome |
| --- | --- | --- |
| CEU-00 | #283 | audit, ADR-050, plan |
| CEU-01 | #284 | evidence refs, context package, untrusted-evidence boundary |
| CEU-02 | #285, fixes #287 | reply binding, bindable notices, upload chat id; the review found a missing foreign key, a non-durable notice guard and a misleading scope string, fixed in `00077` |
| CEU-03 | #288 | recent evidence context, `get_evidence_context`, per-user scope, best-effort context |
| CEU-04 | #291 | no duplicate ledger row, enforced by Go; the unreachable no-reply review binding was removed in review |
| CEU-05 | #292 | merge a document into an existing transaction through opaque candidate refs |
| CEU-06 | #293 | exact replies to proposal-keyed reviews answered at the production entry |
| CEU-07 | this PR | corpus, Operations counters, correction e2e, this record |

Several slices were corrected by review after their first push (an unreachable
binding, a dead redirect on a path production never takes, a missing deterministic
fallback message). The corpus below is what survived those corrections.

---

## 1. Corpus against the CEU-00 test matrix (audit §9)

All run on disposable PostgreSQL 17.4 at the migration head (`go test ./...` and
`go vet ./...` in `apps/worker` and `apps/api`).

| Required case | Test(s) | Slice |
| --- | --- | --- |
| Ref lifetime classes are distinct types; no canonical id in any model-visible payload | `TestReferenceLifetimeClassesAreDistinctTypes`, `TestEvidenceRefFormatIsOpaqueAndDisjointFromOtherRefs`, `TestEvidenceContextIsModelSafeAndKeepsProvenanceSeparate`, `TestPossibleDuplicateEvidenceOffersOpaqueCandidatesAndNoIds`, `TestTurnReplyToUploadShowsTheModelOnlyThatEvidence` | 01, 04, 05 |
| Expired ref → deterministic stale reply, no mutation | `TestExpiredEvidenceRefIsDeterministicAndMutatesNothing`, `TestGetEvidenceContextToolIsAReadOnlyOpaqueRefLookup`, `TestFailedOrMissingEvidenceIsStaleOrNotFound` | 01, 03 |
| Cross-household / cross-user / cross-chat ref rejected | `TestEvidenceRefRejectsOtherHouseholdUserAndChat`, `TestForgedWrongTypeAndMalformedEvidenceRefsAreInvalid`, `TestMergeRefusesRefsThatAreNotTheReviewsCurrentCandidates`, `TestCorrectionRefusesATargetThatIsNotAnIssuedRefForThisHousehold` | 01, 05, 07 |
| Ref issue is idempotent under retry / duplicate delivery | `TestEvidenceRefsIssueResolveAndAreIdempotent`, `TestEvidenceLinkedTransactionRefsDoNotCollide`, `TestNaturalReplyResolvesTheReceiptReviewWithoutReaskingKnownFacts` (replay), `TestEvidenceNoticeIsOnePerDocumentToTheUploadChatAsAReply`, `TestEvidenceNoticeStaysOnePerDocumentAfterJobsArePruned` | 01, 02, 04 |
| Receipt A, receipt B, reply to A "makan" → A only | `TestReplyToUploadBindsOnlyThatEvidence`, `TestTurnReplyToUploadShowsTheModelOnlyThatEvidence`, `TestCategoryReplyToALinkedReceiptStagesACorrectionOnThatTransactionOnly` | 02, 04, 07 |
| Reply to an unresolvable target → no fallback to recent evidence | `TestUnresolvedReplyNeverFallsBackToRecentEvidence`, `TestReplyToUnrelatedMessageDoesNotAnswerTheReview` | 02, 06 |
| Reply binding scoped to chat; album members; bound notices; the database refuses a dangling or cross-household binding | `TestReplyBindingIsScopedToTheChat`, `TestAlbumMemberReplyResolvesToTheAlbumDocument`, `TestReplyToBoundNoticeResolvesItsDocumentAndStaysHouseholdScoped`, `TestMessageBindingRejectsDanglingAndCrossHouseholdDocuments` | 02 |
| Receipt then "makan" (unique) → bound as context | `TestSingleFreshEvidenceBindsAsImmediateContext`, `TestTurnWithoutReplyBindsUniqueRecentEvidenceAsContextOnly`, `TestRecencyWindowsDecideBindingDeterministically` | 03, 04 |
| Receipt A, receipt B, no reply → no arbitrary choice | `TestTwoRecentReceiptsAreAmbiguousAndNeverChosen`, `TestTurnWithTwoRecentReceiptsCarriesCandidatesNotAChoice` | 03, 04 |
| Recent evidence scoped to household, chat and sending user; a broken lookup never fails a message | `TestRecentEvidenceIsScopedToTheChatHouseholdAndUsableDocuments`, `TestRecentEvidenceIsScopedToTheSendingUser`, `TestBrokenRecentEvidenceLookupDegradesToNoContext` | 03 |
| Known amount + user supplies only a date/category → amount not re-decided | `TestNaturalReplyResolvesTheReceiptReviewWithoutReaskingKnownFacts`, `TestDateReplyToTheUploadResolvesTheReviewWithoutReaskingKnownFacts` | 04, 06 |
| Bare "nominalnya 125 ribu" after a receipt → no second transaction, enforced by Go | `TestBareAmountAfterAReceiptIsNeverHarvestedAsASecondTransaction`, `TestModelRecordingTheReceiptAmountAgainIsRefusedByTheLedger`, `TestOnlyFreshEvidenceSuppressesHarvest`, `TestMerchantEquivalenceIsBoundedToWholeDistinctiveWords`, `TestRefusedDuplicateHasADeterministicMessageThatIsNotTheGenericOne` | 04 |
| Receipt linked to a transaction, category correction → the existing transaction, staged not applied | `TestCategoryReplyToALinkedReceiptStagesACorrectionOnThatTransactionOnly`, `TestEvidenceToolPolicyAddsOnlyTheLinkedTransactionCorrection` | 02, 07 |
| "struk ini buat transaksi yang tadi" → merge through the canonical merge, no duplicate | `TestReceiptReplyMergesIntoTheNamedExistingTransactionWithoutADuplicate`, `TestMergeExistingIsOnlyAvailableForPossibleDuplicateReviews`, `TestMergeOutcomesHaveDistinctDeterministicMessages` | 05 |
| Evidence-backed review missing only `transaction_at`; "tanggal 25" → no amount/category re-ask, no model call | `TestDateReplyToTheUploadResolvesTheReviewWithoutReaskingKnownFacts`, `TestDateReplyToABoundNoticeResolvesTheReview`, `TestDateReplyToTheProposalReviewCardIsAnsweredWithoutTheModel`, `TestReplyTargetForEvidenceReviewOnlyRedirectsExactEvidenceReplies`, `TestOnlyExactRepliesToProposalReviewsTakeTheDeterministicLane`, `TestTransactionKeyedReviewRepliesStayWithTheAgent` | 06 |
| Model/provider failure → no human review, no mutation | `TestModelFailureOnAnEvidenceTurnCreatesNoReviewAndNoTransaction` | 04 |
| Prompt injection via caption / extraction / merchant / candidate | `TestWrapUntrustedDefangsEmbeddedBoundaryTags`, `TestWrapUntrustedKeepsPlainTextAndNamesTheTag`, `TestEvidenceContextWrapsAndDefangsHostileEvidenceText`, `TestPromptNamesEvidenceBoundaryAndForbidsAuthorityChange` | 01, 05 |
| Notices: one per document, to the upload chat, atomic with the mutation | `TestAutoConfirmedReceiptQueuesABindableNoticeToTheUploadChat`, `TestLinkedReceiptQueuesALinkedNoticeAndStillCreatesNoDuplicate`, `TestEvidenceNoticeIsSkippedWithoutAnUploadChat`, `TestEvidenceNoticeRollsBackWithItsMutation`, `TestEvidenceNoticeMarkerRollsBackWithItsMutation` | 02 |
| Upload chat id persisted by intake | `TestImageReplayCreatesOneTelegramImageJob`, `TestWebhookCapturesLargestPrivatePhoto` | 02 |
| Binding counters, per household, 30-day window | `TestCEUBindingOutcomesAreCountedPerHouseholdInTheWindow`, `TestCEUTelemetryOutcomesAreAllowListed` | 01, 07 |
| Migrations from zero and rollback | `goose up` → `down` → `up` for `00075`, `00076` and `00077` on every matrix run | 01, 02 |

The standing suites (SAVR corpus, UIR projection, Telegram agent, document
pipeline, native-only LLM guard) ran unchanged in the same matrix.

---

## 2. Metrics

Operations (`/operations` product aggregate) now reports `ceuBinding`, a
30-day count per bounded action name:

| Key | Meaning |
| --- | --- |
| `EXACT_REPLY_BINDING` | a reply bound to its upload, notice, or review card |
| `ACTIVE_REVIEW_BINDING` | a unique active review gained its evidence as context |
| `RECENT_CONTEXT_BINDING` | exactly one recent document was offered as context |
| `AMBIGUOUS_CONTEXT` | several recent documents; the model was given candidates, not a choice |
| `REFERENCE_EXPIRED`, `REFERENCE_INVALID`, `EVIDENCE_NOT_FOUND`, `EVIDENCE_STALE` | resolver refusals |

`SEMANTIC_DISAMBIGUATION` is allow-listed but not produced: the owner chose one
clarification question over a bounded Jev choice (ADR-050).

These are counters of situations, not of user satisfaction. `AMBIGUOUS_CONTEXT`
is recorded when ambiguity exists at context build time, even if that message
turns out not to be about a document, so read it with the existing correction and
rework metrics (`knownFactReaskRate`, auto-confirm corrections, validator-induced
review) rather than alone. No text, amount, or identifier is stored.

---

## 3. Owner-household observation (open)

Use Richmod normally. Do not seed synthetic production data. Acceptance looks at:

1. a reply to an upload or notice reaches the right document and its review;
2. a message right after an upload binds when unique and asks once when not;
3. no second transaction appears for a receipt-related follow-up;
4. no `REFERENCE_*` refusal is surfaced to the household as a confusing answer;
5. the "recorded" notice is welcome in its current wording and frequency;
6. a typed date or amount reply to a payslip or screenshot review card resolves it.

A family not naturally exercised (for example a multi-image album reply, or a
possible-duplicate merge) is recorded `PRODUCTION_UNOBSERVED` with the corpus rows
above as its evidence. Rollback is a normal revert and redeploy; the CEU migrations
(`00075` to `00077`) are additive and reversible.

---

## 4. Known limits (carried honestly)

1. **Amount or merchant of an already recorded transaction cannot be changed from
   chat.** The existing correction tool changes category, description and date
   only, and CEU deliberately adds no finalizer. The model is instructed to say so
   and never claim a change; an unconfirmed receipt's amount, merchant and date are
   answered through its open review.
2. **The clarification wording is model prose.** Tests prove the server never
   chooses and that the candidates are present; they do not pin the sentence.
3. **A reply-less message never binds a document's review.** Chat state alone does
   not own a turn, and the route that would use such a binding (`REVIEW_INTERACTION`)
   is answered terminally by the Jev fast path. This was built in CEU-04, found
   unreachable in review, and removed. Reviews continue from an exact reply (CEU-02,
   CEU-06).
4. **Evidence is Telegram image/document only.** Email evidence reaches a
   conversation through a review or a transaction link only.
5. **Uploads that predate migration `00076` and whose stored update has no readable
   chat id are not reply-bindable.**
6. **There is no CEU kill switch.** The changes are additive; rollback is a revert.
7. **An expired review is not revived from a typed reply.** The agent entry has no
   projection renewal, so an expired card is revived only from a button. A reply to
   an upload or notice does not continue an expired review either.
8. **A pre-existing gap CEU-06 closed, and what it left alone.** Typed replies to
   proposal-keyed review cards (payslip pay date, missing amount) did not resolve in
   production, because the deterministic lane was reachable from callbacks only. They
   now resolve at `ProcessAgent` for exact replies. Reply-less continuation of such a
   review, and revival of an expired one, were not changed.

---

## 5. Freeze

CEU freezes after the observation in §3 is accepted by the owner, on the same
terms as BDR-005: change only for a production defect against the approved
contract. Until then CEU is **corpus-complete, not product-complete**, and this
document is the record of that state.
