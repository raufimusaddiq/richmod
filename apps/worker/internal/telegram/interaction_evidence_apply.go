package telegram

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/financialentity"
)

func evidenceFact(fact factKind) bool {
	switch fact {
	case factDuplicate, factDispute, factEmailAccount, factEmailWealth, factWealthObs, factWealthConfirm, factDocument, factQuality, factCorrection, factUnsupported:
		return true
	}
	return false
}

func evidenceIgnoreLabel(reviewType string) string {
	switch {
	case documentReviewType(reviewType):
		return "dokumen"
	case reviewType == "WEALTH_OBSERVATION_CONFIRMATION":
		return "saldo"
	case reviewType == bankReviewType:
		return "email bank"
	case strings.HasPrefix(reviewType, "FINANCIAL_EMAIL") || reviewType == "CONFLICTING_EVIDENCE":
		return "bukti email"
	}
	return "kartu ini"
}

func (p *Processor) applyEvidenceTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string, sit situation) (stepOutcome, error) {
	if data == callbackIgnore {
		if !stepOffers(step, data) {
			return reask(step, ignoreNotAllowedAck), nil
		}
		return p.applyIgnore(ctx, tx, householdID, sourceEventID, review, step)
	}
	switch sit {
	case sitDuplicate:
		return p.applyDuplicateTap(ctx, tx, householdID, sourceEventID, review, step, data)
	case sitDispute:
		return p.applyDisputeTap(ctx, tx, householdID, sourceEventID, review, step, data)
	case sitEmailEntity:
		return p.applyEmailTap(ctx, tx, householdID, review, step, data)
	case sitWealth:
		return p.applyWealthTap(ctx, tx, householdID, review, step, data)
	case sitDocument:
		return p.applyDocumentTap(ctx, tx, householdID, review, step, data)
	case sitQuality:
		if data == callbackQuality && stepOffers(step, data) {
			return p.confirmReviewedTransaction(ctx, tx, householdID, sourceEventID, review, step, "Total yang tercetak dipakai.")
		}
	case sitCorrection:
		if data == callbackConfirm && stepOffers(step, data) {
			return p.confirmReviewedTransaction(ctx, tx, householdID, sourceEventID, review, step, "Transaksinya dikonfirmasi.")
		}
	}
	return reask(step, choiceStaleAck), nil
}

func (p *Processor) applyEvidenceText(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, text string, targeted bool) (stepOutcome, error) {
	switch step.Fact {
	case factEmailAccount, factEmailWealth, factWealthObs:
		return p.applyEntityText(ctx, tx, householdID, review, step, text, targeted)
	}
	return reask(step, unusableAnswerAck), nil
}

func (p *Processor) applyIgnoreEvidence(ctx context.Context, tx pgx.Tx, review engineReview, step Step) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	if documentReviewType(review.ReviewType) {
		_, err = reviewdomain.ResolveDocumentReview(ctx, sp, reviewdomain.DocumentReviewCommand{
			HouseholdID: review.HouseholdID, UserID: review.UserID, ReviewItemID: review.ItemID, DocumentID: review.DocumentID, ActorType: "TELEGRAM", Action: "IGNORE",
		})
		if errors.Is(err, reviewdomain.ErrDocumentReviewInvalid) {
			_ = sp.Rollback(ctx)
			return reask(step, documentGoneAck), nil
		}
	} else {
		err = reviewdomain.IgnoreReview(ctx, sp, reviewdomain.IgnoreCommand{HouseholdID: review.HouseholdID, ActorUserID: review.UserID, ReviewItemID: review.ItemID, ActorType: "TELEGRAM"})
	}
	switch {
	case errors.Is(err, reviewdomain.ErrAlreadyResolved), errors.Is(err, reviewdomain.ErrIgnoreTransactionBound), errors.Is(err, reviewdomain.ErrFinancialObservationUnavailable):
		_ = sp.Rollback(ctx)
		return reask(step, evidenceChangedAck), nil
	case errors.Is(err, reviewdomain.ErrIgnoreNotAllowed):
		_ = sp.Rollback(ctx)
		return reask(step, ignoreNotAllowedAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: "🗑️ Diabaikan: " + evidenceIgnoreLabel(review.ReviewType) + ". Tidak ada transaksi yang dicatat."}, Settled: true, Changed: true}, nil
}

const evidenceChangedAck = "Kartu ini sudah berubah, jadi belum ada yang diabaikan."

func (p *Processor) applyDuplicateTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	switch {
	case strings.HasPrefix(data, callbackDupPg):
		page, ok := parsePageCallback(data, callbackDupPg)
		if !ok {
			return reask(step, choiceStaleAck), nil
		}
		if err := step.loadDuplicateRows(ctx, tx, review.RequestID, page); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
	case strings.HasPrefix(data, callbackDupMerge):
		return p.mergeDuplicate(ctx, tx, householdID, review, step, data)
	case data == callbackDupNew && stepOffers(step, data):
		return p.recordDuplicateAsNew(ctx, tx, householdID, sourceEventID, review, step)
	}
	return reask(step, choiceStaleAck), nil
}

func (p *Processor) mergeDuplicate(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, data string) (stepOutcome, error) {
	view, err := loadDuplicateView(ctx, tx, review.RequestID)
	if err != nil {
		return stepOutcome{}, err
	}
	if !view.mergeAllowed() {
		return reask(step, choiceStaleAck), nil
	}
	targetID, ok, err := resolveStampedAccount(ctx, tx, review.RequestID, duplicateStampKey, data, callbackDupMerge, view.choices())
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return reask(step, duplicateStaleAck), nil
	}
	var target duplicateRecord
	for _, candidate := range view.Candidates {
		if candidate.ID == targetID {
			target = candidate
		}
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	_, err = reviewdomain.MergeDuplicateReview(ctx, sp, reviewdomain.DuplicateCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID, TargetTransactionID: targetID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID, Action: "TELEGRAM_MERGE_REVIEW",
	})
	switch {
	case errors.Is(err, reviewdomain.ErrDuplicateTargetInvalid), errors.Is(err, reviewdomain.ErrAlreadyMerged), errors.Is(err, reviewdomain.ErrAlreadyResolved), errors.Is(err, reviewdomain.ErrEvidenceDisputeConfirmationRequired):
		_ = sp.Rollback(ctx)
		return reask(step, duplicateStaleAck), nil
	case errors.Is(err, reviewdomain.ErrMergeTargetActive):
		_ = sp.Rollback(ctx)
		return reask(step, mergeTargetActiveAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: "✅ Digabung dengan transaksi yang sudah ada: " + target.line() + "."}, Settled: true, Changed: true}, nil
}

func (p *Processor) recordDuplicateAsNew(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	remaining, err := reviewdomain.ApplyTransactionReviewFacts(ctx, sp, reviewdomain.ReviewFacts{HouseholdID: householdID, TransactionID: review.TransactionID, Relationship: "DISTINCT"})
	if errors.Is(err, pgx.ErrNoRows) {
		_ = sp.Rollback(ctx)
		return reask(step, duplicateStaleAck), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if len(remaining) > 0 {
		if _, err := sp.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'TELEGRAM',$2,'REVIEW_DISTINCT_EVENT','transaction',$3)`, householdID, review.UserID, review.TransactionID); err != nil {
			_ = sp.Rollback(ctx)
			return stepOutcome{}, err
		}
		if err := sp.Commit(ctx); err != nil {
			return stepOutcome{}, err
		}
		next, err := p.reloadedStep(ctx, tx, householdID, review)
		if err != nil {
			return stepOutcome{}, err
		}
		next.Step = next.Step.WithAck("✅ Tersimpan sebagai transaksi baru.")
		return next, nil
	}
	return p.finishConfirm(ctx, tx, sp, householdID, sourceEventID, review, step, "TELEGRAM_CONFIRM_AS_NEW", "Tersimpan sebagai transaksi baru.")
}

func (p *Processor) confirmReviewedTransaction(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, note string) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	return p.finishConfirm(ctx, tx, sp, householdID, sourceEventID, review, step, "TELEGRAM_CONFIRMED", note)
}

func (p *Processor) finishConfirm(ctx context.Context, tx, sp pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, action, note string) (stepOutcome, error) {
	_, err := reviewdomain.ConfirmTransactionReview(ctx, sp, reviewdomain.ConfirmCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID, Action: action, ResolveReview: true,
	})
	var missing *reviewdomain.ErrMissingFacts
	switch {
	case errors.Is(err, reviewdomain.ErrMissingCategory):
		_ = sp.Rollback(ctx)
		outcome, err := p.enterCategoryStep(ctx, tx, householdID, review)
		if err != nil {
			return stepOutcome{}, err
		}
		outcome.Step = outcome.Step.WithAck(categoryNeededAck)
		return outcome, nil
	case errors.As(err, &missing), errors.Is(err, reviewdomain.ErrTransferClassificationRequired), errors.Is(err, reviewdomain.ErrAlreadyResolved):
		_ = sp.Rollback(ctx)
		return reask(step, choiceStaleAck), nil
	case reviewdomain.IsMerchantLinkProblem(err):
		_ = sp.Rollback(ctx)
		reply, _ := merchantLinkProblemReply(err)
		return reask(step, reply), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := reviewdomain.PromoteEvidenceDocuments(ctx, sp, householdID, review.TransactionID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,metadata_json) VALUES($1,$2,'TELEGRAM_REVIEW_REPLY',jsonb_build_object('review_request_id',$3::uuid,'classification','CONFIRM')) ON CONFLICT DO NOTHING`, review.TransactionID, sourceEventID, review.RequestID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return p.confirmedDone(ctx, tx, householdID, review, note)
}

func (p *Processor) applyDisputeTap(ctx context.Context, tx pgx.Tx, householdID, sourceEventID string, review engineReview, step Step, data string) (stepOutcome, error) {
	switch {
	case data == callbackDisputeReject && stepOffers(step, data):
		return p.resolveDispute(ctx, tx, householdID, review, step, false, "")
	case step.Fact == factCategory && strings.HasPrefix(data, callbackCategoryPg):
		page, ok := parsePageCallback(data, callbackCategoryPg)
		if !ok {
			return reask(step, choiceStaleAck), nil
		}
		return p.categoryPage(ctx, tx, review, step, page)
	case step.Fact == factCategory && strings.HasPrefix(data, callbackCategory):
		return p.resolveDispute(ctx, tx, householdID, review, step, true, strings.TrimPrefix(data, callbackCategory))
	case data == callbackDisputeConfirm && step.Fact == factDispute && stepOffers(step, data):
		return p.resolveDispute(ctx, tx, householdID, review, step, true, "")
	}
	return reask(step, choiceStaleAck), nil
}

func (p *Processor) resolveDispute(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, confirm bool, categoryID string) (stepOutcome, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = reviewdomain.ResolveEvidenceDispute(ctx, sp, reviewdomain.EvidenceDisputeCommand{
		HouseholdID: householdID, ActorUserID: review.UserID, TransactionID: review.TransactionID,
		ReviewItemID: review.ItemID, RequestID: review.RequestID, CategoryID: categoryID,
	}, confirm)
	switch {
	case errors.Is(err, reviewdomain.ErrMissingCategory):
		_ = sp.Rollback(ctx)
		base, err := p.baseStep(ctx, tx, householdID, review)
		if err != nil {
			return stepOutcome{}, err
		}
		return p.moveToStep(ctx, tx, review, disputeCategoryStep(base))
	case errors.Is(err, reviewdomain.ErrInvalidCandidate):
		_ = sp.Rollback(ctx)
		return reask(step, categoryInactiveAck), nil
	case errors.Is(err, reviewdomain.ErrMergeTargetActive):
		_ = sp.Rollback(ctx)
		return reask(step, disputeMergeActiveAck), nil
	case errors.Is(err, reviewdomain.ErrEvidenceDisputeInvalid), errors.Is(err, reviewdomain.ErrAlreadyResolved):
		_ = sp.Rollback(ctx)
		return reask(step, disputeStaleAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	ack := "✅ Koreksi dibatalkan. Screenshot tetap terkait dengan transaksi semula, dan tidak ada transaksi baru."
	if confirm {
		ack = "✅ Tersimpan sebagai transaksi berbeda. Screenshot kini terkait dengan transaksi baru, dan transaksi lama beserta bukti aslinya tetap ada."
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: ack}, Settled: true, Changed: true}, nil
}

func (p *Processor) applyEmailTap(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, data string) (stepOutcome, error) {
	spec := stampSpecFor(step.Fact)
	if page, ok := parsePageCallback(data, spec.PagePrefix); ok {
		if err := step.loadEntityRows(ctx, tx, review.RequestID, page); err != nil {
			return stepOutcome{}, err
		}
		return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
	}
	if !strings.HasPrefix(data, spec.Prefix) {
		return reask(step, choiceStaleAck), nil
	}
	current, err := emailChoices(ctx, tx, householdID, step.Fact)
	if err != nil {
		return stepOutcome{}, err
	}
	id, ok, err := resolveStampedAccount(ctx, tx, review.RequestID, spec.Key, data, spec.Prefix, current)
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return reask(step, accountStaleAck), nil
	}
	return p.resolveEmailEntity(ctx, tx, householdID, review, step, id)
}

func (p *Processor) applyEntityText(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, text string, targeted bool) (stepOutcome, error) {
	hint := strings.TrimSpace(text)
	if hint == "" || len([]rune(hint)) > 80 || strings.Contains(hint, "?") || isYesNoAnswer(hint) {
		return reask(step, unusableAnswerAck), nil
	}
	choices, err := emailChoices(ctx, tx, householdID, step.Fact)
	if err != nil {
		return stepOutcome{}, err
	}
	resolve := financialentity.Account
	if step.Fact != factEmailAccount {
		resolve = financialentity.WealthAccount
	}
	id, ok, err := entityFromText(ctx, tx, householdID, hint, targeted, choices, resolve)
	if err != nil {
		return stepOutcome{}, err
	}
	if !ok {
		return reask(step, "Aku belum menemukan satu akun yang cocok dengan “"+clean(hint, 40)+"”."), nil
	}
	if step.Fact == factWealthObs {
		return p.resolveWealth(ctx, tx, householdID, review, step, id)
	}
	return p.resolveEmailEntity(ctx, tx, householdID, review, step, id)
}

func (p *Processor) resolveEmailEntity(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, entityID string) (stepOutcome, error) {
	command := reviewdomain.FinancialEmailCommand{HouseholdID: householdID, ObservationID: review.ObservationID, ReviewItemID: review.ItemID, ActorUserID: review.UserID, ActorType: "TELEGRAM"}
	ack := "✅ Rekening dipilih."
	if step.Fact == factEmailAccount {
		command.AccountID = entityID
	} else {
		command.WealthAccountID, ack = entityID, "✅ Akun kekayaan dipilih."
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	result, err := reviewdomain.ResolveFinancialEmailReview(ctx, sp, command)
	switch {
	case errors.Is(err, reviewdomain.ErrAccountInvalid), errors.Is(err, reviewdomain.ErrWealthAccountInvalid):
		_ = sp.Rollback(ctx)
		if err := step.loadEntityRows(ctx, tx, review.RequestID, 0); err != nil {
			return stepOutcome{}, err
		}
		return reask(step.executable(cardActions(review.Decision, review.ReviewType)), accountStaleAck), nil
	case errors.Is(err, reviewdomain.ErrFinancialObservationUnavailable):
		_ = sp.Rollback(ctx)
		return reask(step, emailGoneAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'RESOLVE_FINANCIAL_EMAIL_ENTITIES','financial_email_observation',$3,jsonb_build_object('accountId',$4::text,'wealthAccountId',$5::text))`, householdID, review.UserID, review.ObservationID, result.AccountID, result.WealthAccountID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	if !result.Complete {
		next, err := p.reloadedStep(ctx, tx, householdID, review)
		if err != nil {
			return stepOutcome{}, err
		}
		next.Step = next.Step.WithAck(ack)
		return next, nil
	}
	var account, wealth string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT name FROM account WHERE id=NULLIF($1,'')::uuid),''),COALESCE((SELECT name FROM wealth_account WHERE id=NULLIF($2,'')::uuid),'')`, result.AccountID, result.WealthAccountID).Scan(&account, &wealth); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: savedLine(account, wealth), Hint: "Email ini sedang diproses lagi dengan pilihanmu."}, Settled: true, Changed: true}, nil
}

func (p *Processor) applyWealthTap(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, data string) (stepOutcome, error) {
	switch {
	case step.Fact == factWealthConfirm && data == callbackConfirm:
		known, _ := review.Decision.KnownFacts["wealth_account"].(string)
		if known == "" {
			return reask(step, choiceStaleAck), nil
		}
		return p.resolveWealth(ctx, tx, householdID, review, step, known)
	case step.Fact == factWealthConfirm && data == callbackChange:
		return p.moveToStep(ctx, tx, review, wealthPickStep(step))
	case step.Fact == factWealthObs:
		if page, ok := parsePageCallback(data, callbackWealthPg); ok {
			if err := step.loadEntityRows(ctx, tx, review.RequestID, page); err != nil {
				return stepOutcome{}, err
			}
			return stepOutcome{Step: step.executable(cardActions(review.Decision, review.ReviewType))}, nil
		}
		if !strings.HasPrefix(data, callbackWealthAccount) {
			return reask(step, choiceStaleAck), nil
		}
		current, err := emailChoices(ctx, tx, householdID, step.Fact)
		if err != nil {
			return stepOutcome{}, err
		}
		id, ok, err := resolveStampedAccount(ctx, tx, review.RequestID, wealthStampKey, data, callbackWealthAccount, current)
		if err != nil {
			return stepOutcome{}, err
		}
		if !ok {
			return reask(step, accountStaleAck), nil
		}
		return p.resolveWealth(ctx, tx, householdID, review, step, id)
	}
	return reask(step, choiceStaleAck), nil
}

func (p *Processor) resolveWealth(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, accountID string) (stepOutcome, error) {
	var hint, value string
	if err := tx.QueryRow(ctx, `SELECT account_hint,round(observed_value_idr)::text FROM wealth_observation WHERE id=$1 AND household_id=$2`, review.WealthID, householdID).Scan(&hint, &value); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reask(step, wealthGoneAck), nil
		}
		return stepOutcome{}, err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	err = reviewdomain.ResolveWealthObservation(ctx, sp, reviewdomain.WealthObservationCommand{
		HouseholdID: householdID, ObservationID: review.WealthID, WealthAccountID: accountID,
		Alias: financialentity.Normalize(hint), ActorUserID: review.UserID, ActorType: "TELEGRAM",
	})
	switch {
	case errors.Is(err, reviewdomain.ErrWealthAccountInvalid):
		_ = sp.Rollback(ctx)
		if step.Fact == factWealthConfirm {
			return p.moveToStep(ctx, tx, review, wealthPickStep(step))
		}
		if err := step.loadEntityRows(ctx, tx, review.RequestID, 0); err != nil {
			return stepOutcome{}, err
		}
		return reask(step.executable(cardActions(review.Decision, review.ReviewType)), accountStaleAck), nil
	case errors.Is(err, reviewdomain.ErrWealthObservationNotFound):
		_ = sp.Rollback(ctx)
		return reask(step, wealthGoneAck), nil
	case err != nil:
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM wealth_account WHERE id=$1 AND household_id=$2`, accountID, householdID).Scan(&name); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: "✅ Saldo tersimpan: " + joinDot(name, IDR(value)), Hint: balanceNote}, Settled: true, Changed: true}, nil
}

func (p *Processor) applyDocumentTap(ctx context.Context, tx pgx.Tx, householdID string, review engineReview, step Step, data string) (stepOutcome, error) {
	if data != callbackReprocess || !stepOffers(step, data) {
		return reask(step, choiceStaleAck), nil
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return stepOutcome{}, err
	}
	_, err = reviewdomain.ResolveDocumentReview(ctx, sp, reviewdomain.DocumentReviewCommand{
		HouseholdID: householdID, UserID: review.UserID, ReviewItemID: review.ItemID, DocumentID: review.DocumentID, ActorType: "TELEGRAM", Action: "REPROCESS_DOCUMENT",
	})
	if errors.Is(err, reviewdomain.ErrDocumentReviewInvalid) {
		_ = sp.Rollback(ctx)
		return reask(step, documentGoneAck), nil
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `INSERT INTO job(type,payload_json) SELECT 'PROCESS_DOCUMENT',jsonb_build_object('document_id',$1::uuid) WHERE NOT EXISTS(SELECT 1 FROM job WHERE type='PROCESS_DOCUMENT' AND payload_json->>'document_id'=$1::text AND status IN ('PENDING','RUNNING'))`, review.DocumentID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if _, err := sp.Exec(ctx, `UPDATE review_conversation SET context_json=context_json||jsonb_build_object('reprocess',jsonb_build_object('document_id',$2::text)),updated_at=now() WHERE review_request_id=$1`, review.RequestID, review.DocumentID); err != nil {
		_ = sp.Rollback(ctx)
		return stepOutcome{}, err
	}
	if err := sp.Commit(ctx); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{Step: Step{Kind: stepDone, Ack: "⏳ Dokumen sedang dibaca ulang.", Hint: "Hasilnya muncul di kartu ini."}, Settled: true, Changed: true}, nil
}
