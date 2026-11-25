package telegram

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

const maxLiveCards = 10

type cardAnswerKind string

const (
	cardAnswerNone     cardAnswerKind = ""
	cardAnswerCategory cardAnswerKind = "category_slug"
	cardAnswerDate     cardAnswerKind = "transaction_at"
	cardAnswerAmount   cardAnswerKind = "amount_idr"
	cardAnswerDetail   cardAnswerKind = "detail"
	cardAnswerConfirm  cardAnswerKind = "confirmation"
	cardAnswerAccount  cardAnswerKind = "account"
)

const cardKindProposal = "PROPOSAL"

type typedCardAnswer struct {
	Kind  cardAnswerKind
	Value string
}

func expectedCardAnswer(card agentReviewBinding) cardAnswerKind {
	if step, ok := stepForBinding(card); ok {
		return step.AnswerKind()
	}
	switch {
	case card.Kind == cardKindProposal:
		return proposalExpectedAnswer(card)
	case card.Kind == "BANK_FACTS":
		return cardAnswerAmount
	case card.Kind != "TRANSACTION":
		return cardAnswerNone
	case card.ConversationState == "AWAITING_CATEGORY":
		return cardAnswerCategory
	case card.ConversationState == "AWAITING_DATE":
		return cardAnswerDate
	case card.ConversationState == "AWAITING_MERCHANT", card.ConversationState == "AWAITING_DETAIL", card.ReviewType == "UNKNOWN_PURPOSE":
		return cardAnswerDetail
	}
	return cardAnswerNone
}

func proposalExpectedAnswer(card agentReviewBinding) cardAnswerKind {
	switch {
	case card.ConversationState == "AWAITING_DATE" && (card.ReviewType == "PAYSLIP_CONFIRMATION" || card.ReviewType == "MISSING_PAY_DATE"):
		return cardAnswerDate
	case card.ReviewType == "MISSING_AMOUNT" && card.ConversationState == "AWAITING_CONFIRMATION":
		return cardAnswerConfirm
	case card.ReviewType == "MISSING_AMOUNT" && card.ConversationState == "AWAITING_DETAIL":
		return cardAnswerAmount
	}
	return cardAnswerNone
}

func (p *Processor) loadWaitingCards(ctx context.Context, householdID string, chatID int64) ([]agentReviewBinding, error) {
	cards, err := p.loadLiveReviewCards(ctx, householdID, chatID)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT ri.id::text,r.id::text,ri.id::text,ri.review_type,COALESCE(c.state,''),rr.telegram_message_id,
			COALESCE(p.amount::text,''),COALESCE(NULLIF(p.counterparty_raw,''),NULLIF(p.merchant_raw,''),'Slip gaji')
		FROM review_request r
		JOIN review_item ri ON ri.id=r.review_item_id AND ri.household_id=r.household_id
		JOIN transaction_proposal p ON p.id=ri.proposal_id
		JOIN review_request_recipient rr ON rr.review_request_id=r.id
		LEFT JOIN review_conversation c ON c.review_request_id=r.id
		WHERE r.household_id=$1 AND r.status='OPEN' AND r.expires_at>now() AND rr.telegram_chat_id=$2 AND rr.telegram_message_id IS NOT NULL
		  AND ri.status IN ('OPEN','PENDING_SEND') AND ri.transaction_id IS NULL AND r.transaction_id IS NULL
		ORDER BY r.created_at DESC LIMIT $3::int`, householdID, chatID, maxLiveCards)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for _, card := range cards {
		seen[card.ReviewRequestID] = true
	}
	for rows.Next() {
		card := agentReviewBinding{Kind: cardKindProposal}
		if err := rows.Scan(&card.TargetID, &card.ReviewRequestID, &card.ReviewItemID, &card.ReviewType, &card.ConversationState, &card.TelegramMessageID, &card.AmountIDR, &card.Label); err != nil {
			return nil, err
		}
		if !seen[card.ReviewRequestID] {
			cards = append(cards, card)
		}
	}
	return cards, rows.Err()
}

func (p *Processor) loadLiveReviewCards(ctx context.Context, householdID string, chatID int64) ([]agentReviewBinding, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT kind,target_id,review_request_id,transaction_id,review_type,conversation_state,merchant_id,message_id,amount_idr,label,review_item_id FROM (
		SELECT DISTINCT ON (review_request_id) kind,target_id,review_request_id,transaction_id,review_type,conversation_state,merchant_id,message_id,amount_idr,label,review_item_id,sort_at
		FROM (
			SELECT 'TRANSACTION'::text,r.id::text,r.id::text,t.id::text,r.review_type,
			       COALESCE(c.state,''),COALESCE(t.merchant_id::text,''),COALESCE(rr.telegram_message_id,0)::bigint,
			       t.amount::text,COALESCE(t.counterparty_name,t.description,'Transaksi'),r.created_at,r.expires_at,r.review_item_id::text
			FROM review_request r
			JOIN transaction t ON t.id=r.transaction_id
			JOIN review_conversation c ON c.review_request_id=r.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE r.household_id=$1 AND r.status='OPEN' AND t.status='NEEDS_REVIEW' AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'TRANSFER_RECONCILIATION',trc.id::text,r.id::text,'',r.review_type,'','',COALESCE(rr.telegram_message_id,0)::bigint,
			       trc.amount_idr::text,COALESCE(trc.description,'Transfer'),r.created_at,r.expires_at,r.review_item_id::text
			FROM transfer_reconciliation_case trc
			JOIN review_item ri ON (
				(ri.source_event_id IS NOT NULL AND ri.source_event_id=trc.source_event_id)
				OR (ri.financial_email_observation_id IS NOT NULL AND ri.financial_email_observation_id=trc.financial_email_observation_id)
			)
			JOIN review_request r ON r.review_item_id=ri.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE trc.household_id=$1 AND trc.status='OPEN' AND ri.status IN ('PENDING_SEND','OPEN')
			  AND r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'WEALTH_OBSERVATION',wo.id::text,r.id::text,'',r.review_type,'','',COALESCE(rr.telegram_message_id,0)::bigint,
			       wo.observed_value_idr::text,trim(wo.institution||' '||wo.account_hint),r.created_at,r.expires_at,r.review_item_id::text
			FROM wealth_observation wo
			JOIN review_item ri ON ri.wealth_observation_id=wo.id
			JOIN review_request r ON r.review_item_id=ri.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE wo.household_id=$1 AND wo.status='PENDING' AND ri.status IN ('PENDING_SEND','OPEN')
			  AND r.household_id=$1 AND r.status='OPEN' AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'CYCLE_RESIDUAL',crc.id::text,r.id::text,'',r.review_type,'','',COALESCE(rr.telegram_message_id,0)::bigint,
			       crc.basis_residual_idr::text,'Sisa siklus gaji',r.created_at,r.expires_at,r.review_item_id::text
			FROM review_request r
			JOIN review_item ri ON ri.id=r.review_item_id
			JOIN cycle_residual_case crc ON crc.id=ri.cycle_residual_case_id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE r.household_id=$1 AND r.review_type='CYCLE_RESIDUAL_ALLOCATION' AND r.status='OPEN'
			  AND ri.status IN ('PENDING_SEND','OPEN') AND rr.telegram_chat_id=$2
			UNION ALL
			SELECT 'BANK_FACTS',ri.id::text,r.id::text,'',r.review_type,COALESCE(c.state,''),'',COALESCE(rr.telegram_message_id,0)::bigint,
			       '','Email bank',r.created_at,r.expires_at,r.review_item_id::text
			FROM review_request r
			JOIN review_item ri ON ri.id=r.review_item_id
			LEFT JOIN review_conversation c ON c.review_request_id=r.id
			JOIN review_request_recipient rr ON rr.review_request_id=r.id
			WHERE r.household_id=$1 AND r.status='OPEN' AND ri.review_type='UNKNOWN_BANK_TEMPLATE'
			  AND ri.transaction_id IS NULL AND ri.source_event_id IS NOT NULL
			  AND ri.status IN ('PENDING_SEND','OPEN') AND rr.telegram_chat_id=$2
		) candidates(kind,target_id,review_request_id,transaction_id,review_type,conversation_state,merchant_id,message_id,amount_idr,label,sort_at,expires_at,review_item_id)
		WHERE expires_at>now()
		-- One card per review request: a transfer case can match its review item
		-- through both the source event and the email observation.
		ORDER BY review_request_id,sort_at DESC
		) unique_cards
		ORDER BY sort_at DESC
		LIMIT $3::int`, householdID, chatID, maxLiveCards)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := make([]agentReviewBinding, 0, 4)
	for rows.Next() {
		var card agentReviewBinding
		if err := rows.Scan(&card.Kind, &card.TargetID, &card.ReviewRequestID, &card.TransactionID,
			&card.ReviewType, &card.ConversationState, &card.MerchantID, &card.TelegramMessageID,
			&card.AmountIDR, &card.Label, &card.ReviewItemID); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

func (p *Processor) categoryNames(ctx context.Context, householdID string) (map[string]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT slug,name FROM category WHERE household_id=$1 AND active`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var slug, name string
		if err := rows.Scan(&slug, &name); err != nil {
			return nil, err
		}
		names[slug] = name
	}
	return names, rows.Err()
}

func matchTypedCardAnswer(text string, cards []agentReviewBinding, categories map[string]string, accounts map[string][]accountChoice, now time.Time) ([]agentReviewBinding, typedCardAnswer) {
	var matched []agentReviewBinding
	var answer typedCardAnswer
	var detailCards []agentReviewBinding
	for _, card := range cards {
		kind := expectedCardAnswer(card)
		value, ok := "", false
		switch kind {
		case cardAnswerCategory:
			value, ok = parseCategoryAnswer(text, categories)
		case cardAnswerAccount:
			if choice, exact := exactAccountMatch(text, accounts[card.ConversationState]); exact {
				value, ok = normalizeAnswer(choice.Name), true
			}
		case cardAnswerDate:
			if parsed, valid := parseDateValue(text, now, 0); valid {
				value, ok = parsed.Format("2006-01-02"), true
			}
		case cardAnswerAmount:
			value, ok = parseBankFactsAnswer(text)
		case cardAnswerConfirm:
			value, ok = normalizeAnswer(text), isYesNoAnswer(text) || incomeReviewChoice(text) != ""
		case cardAnswerDetail:
			detailCards = append(detailCards, card)
		}
		if !ok {
			continue
		}
		if answer.Kind != "" && (answer.Kind != kind || answer.Value != value) {
			return nil, typedCardAnswer{}
		}
		answer = typedCardAnswer{Kind: kind, Value: value}
		matched = append(matched, card)
	}
	if len(matched) > 0 {
		return matched, answer
	}
	if len(detailCards) == 1 && isDetailAnswer(text, now) {
		return detailCards, typedCardAnswer{Kind: cardAnswerDetail, Value: strings.TrimSpace(text)}
	}
	return nil, typedCardAnswer{}
}

func isDetailAnswer(text string, now time.Time) bool {
	value := normalizeAnswer(text)
	if value == "" || strings.ContainsAny(value, "?\n") || len(strings.Fields(value)) > 6 || len([]rune(value)) > 80 {
		return false
	}
	for _, r := range value {
		if unicode.IsDigit(r) {
			return false
		}
	}
	if isYesNoAnswer(value) || isHelpCommand(value) || strings.HasPrefix(value, "/") || mentionsFinanceRequest(value) {
		return false
	}
	if _, ok := parseDateValue(value, now, 0); ok {
		return false
	}
	return true
}

var financeRequestWords = map[string]bool{
	"pengeluaran": true, "pemasukan": true, "pendapatan": true, "cek": true, "saldo": true, "laporan": true, "catat": true,
	"bandingkan": true, "banding": true, "berapa": true, "total": true, "ringkasan": true, "rekap": true, "tampilkan": true,
	"lihat": true, "tabungan": true, "kekayaan": true, "cashflow": true, "spending": true, "expense": true, "income": true,
	"balance": true, "report": true, "compare": true, "show": true, "bantuan": true, "ubah": true, "koreksi": true,
	"hapus": true, "batalkan": true, "cari": true, "cariin": true, "transaksi": true, "gaji": true, "siklus": true, "kategori": true,
}

func mentionsFinanceRequest(value string) bool {
	for _, word := range strings.Fields(value) {
		if financeRequestWords[word] {
			return true
		}
	}
	return false
}

func normalizeAnswer(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.TrimRightFunc(text, func(r rune) bool { return r == '.' || r == '!' || unicode.IsSpace(r) })
	return strings.Join(strings.Fields(text), " ")
}

var answerFillerWords = map[string]bool{
	"masukin": true, "masukkan": true, "masukan": true, "ke": true, "aja": true, "saja": true, "kategori": true, "kategorinya": true,
	"jadi": true, "itu": true, "ini": true, "yang": true, "buat": true, "untuk": true, "deh": true, "ya": true, "dong": true, "sih": true,
	"pakai": true, "pake": true, "taruh": true, "catat": true, "dicatat": true, "sebagai": true, "pilih": true, "di": true, "tolong": true,
	"tanggal": true, "tanggalnya": true, "tgl": true, "nominal": true, "nominalnya": true, "jumlah": true, "jumlahnya": true,
}

func stripAnswerFiller(text string) string {
	words := strings.Fields(strings.NewReplacer(",", " ", ":", " ").Replace(normalizeAnswer(text)))
	kept := words[:0]
	for _, word := range words {
		if !answerFillerWords[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

func parseCategoryAnswer(text string, categories map[string]string) (string, bool) {
	value := stripAnswerFiller(text)
	if value == "" {
		return "", false
	}
	found := ""
	for slug, name := range categories {
		candidates := []string{strings.ToLower(slug), strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(slug)), stripAnswerFiller(name), normalizeAnswer(name)}
		for _, candidate := range candidates {
			if candidate != "" && value == candidate {
				if found != "" && found != slug {
					return "", false
				}
				found = slug
			}
		}
	}
	return found, found != ""
}

var bankFactsTimeWords = map[string]bool{
	"tadi": true, "pagi": true, "siang": true, "sore": true, "malam": true, "jam": true, "pukul": true, "wib": true,
	"kemarin": true, "kemaren": true, "hari": true, "ini": true, "tanggal": true, "tgl": true, "rp": true, "rp.": true,
}

var clockToken = regexp.MustCompile(`^([01]?[0-9]|2[0-3])[:.][0-5][0-9]$`)

func parseBankFactsAnswer(text string) (string, bool) {
	fields := strings.Fields(strings.NewReplacer(",", " ").Replace(normalizeAnswer(text)))
	amount := ""
	for index := 0; index < len(fields); index++ {
		token := fields[index]
		switch {
		case clockToken.MatchString(token), bankFactsTimeWords[token], monthNames[token] != 0, answerFillerWords[token]:
			continue
		case amountToken.MatchString(token):
			unit := ""
			if index+1 < len(fields) {
				if next, ok := amountUnits[fields[index+1]]; ok {
					unit = next
					index++
				}
			}
			value, ok := parseAmountToken(token, unit)
			if !ok {
				if index+1 < len(fields) && monthNames[fields[index+1]] != 0 {
					continue
				}
				return "", false
			}
			if amount != "" && amount != value {
				return "", false
			}
			amount = value
		default:
			return "", false
		}
	}
	return amount, amount != ""
}

var yesNoAnswers = map[string]bool{
	"ya": true, "iya": true, "iyaa": true, "iyap": true, "yes": true, "y": true, "ok": true, "oke": true, "okay": true, "boleh": true,
	"betul": true, "benar": true, "setuju": true, "simpan": true, "ingat": true, "ingat ya": true, "ya simpan": true, "iya simpan": true,
	"tidak": true, "tdk": true, "gak": true, "ga": true, "nggak": true, "ngga": true, "enggak": true, "no": true, "jangan": true,
	"tidak usah": true, "ga usah": true, "gak usah": true, "nggak usah": true, "sekali ini saja": true, "sekali ini aja": true,
}

func isYesNoAnswer(text string) bool { return yesNoAnswers[normalizeAnswer(text)] }

func hasProposalCard(cards []agentReviewBinding) bool {
	for _, card := range cards {
		if card.Kind == cardKindProposal {
			return true
		}
	}
	return false
}

const callbackPendingCardPrefix = "pending:card:"

func (p *Processor) askWhichCard(ctx context.Context, householdID, sourceEventID string, update telegramUpdate, cards []agentReviewBinding, answer typedCardAnswer) error {
	if (answer.Kind != cardAnswerCategory && answer.Kind != cardAnswerDate) || hasProposalCard(cards) {
		return p.finishWithoutTransaction(ctx, sourceEventID, "PROCESSED", update, "Ada beberapa kartu yang menunggu jawaban seperti itu. Balas (Reply) kartu yang dimaksud langsung, ya.")
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var choiceID string
	if err := tx.QueryRow(ctx, `INSERT INTO telegram_pending_card_choice(household_id,telegram_user_id,telegram_chat_id,source_event_id,answer_field,answer_value)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(source_event_id) DO UPDATE SET answer_value=excluded.answer_value RETURNING id::text`,
		householdID, update.Message.From.ID, update.Message.Chat.ID, sourceEventID, string(answer.Kind), answer.Value).Scan(&choiceID); err != nil {
		return err
	}
	keyboard := make([][]InlineKeyboardButton, 0, len(cards))
	for index, card := range cards {
		var token string
		if err := tx.QueryRow(ctx, `INSERT INTO telegram_pending_card_choice_option(choice_id,position,review_request_id) VALUES($1,$2,$3)
			ON CONFLICT(choice_id,position) DO UPDATE SET review_request_id=excluded.review_request_id RETURNING token::text`, choiceID, index+1, card.ReviewRequestID).Scan(&token); err != nil {
			return err
		}
		label := clean(card.Label, 40)
		if card.AmountIDR != "" {
			label += " · Rp" + FormatIDR(card.AmountIDR)
		}
		keyboard = append(keyboard, []InlineKeyboardButton{{Text: label, CallbackData: callbackPendingCardPrefix + token}})
	}
	shown := answer.Value
	if answer.Kind == cardAnswerDate {
		if at, err := time.ParseInLocation("2006-01-02", answer.Value, jakartaLocation()); err == nil {
			shown = formatIDDate(at)
		}
	}
	message := fmt.Sprintf("Jawaban “%s” cocok untuk %d kartu yang masih terbuka. Untuk yang mana?", clean(update.Message.Text, 60), len(cards))
	if answer.Kind == cardAnswerDate {
		message = fmt.Sprintf("Tanggal %s cocok untuk %d kartu yang masih terbuka. Untuk yang mana?", shown, len(cards))
	}
	if _, err := tx.Exec(ctx, `UPDATE source_event SET processing_status='PROCESSED',parser_name='telegram-agent-card-choice',parser_version='1' WHERE id=$1`, sourceEventID); err != nil {
		return err
	}
	if err := enqueueReplyMarkup(ctx, tx, update, message, &InlineKeyboardMarkup{InlineKeyboard: keyboard}); err != nil {
		return err
	}
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "USER", update.Message.Text, "", map[string]any{"current_jakarta_datetime": p.turnTime(update).Format(time.RFC3339)}); err != nil {
		return err
	}
	if err := insertTurnTx(ctx, tx, householdID, sourceEventID, update, "ASSISTANT", message, "", map[string]any{"card_choice": len(cards)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const cardChoiceStaleMessage = "Pilihan ini sudah tidak berlaku. Balas (Reply) kartu yang dimaksud langsung, ya."

func (p *Processor) processCardChoiceCallback(ctx context.Context, sourceEventID, householdID string, update telegramUpdate, data string) (bool, error) {
	if !strings.HasPrefix(data, callbackPendingCardPrefix) {
		return false, nil
	}
	token := strings.TrimPrefix(data, callbackPendingCardPrefix)
	var choiceID, field, value, reviewRequestID string
	var cardMessageID int64
	err := p.pool.QueryRow(ctx, `SELECT c.id::text,c.answer_field,c.answer_value,o.review_request_id::text,COALESCE(rr.telegram_message_id,0)::bigint
		FROM telegram_pending_card_choice_option o
		JOIN telegram_pending_card_choice c ON c.id=o.choice_id
		LEFT JOIN review_request_recipient rr ON rr.review_request_id=o.review_request_id AND rr.telegram_chat_id=c.telegram_chat_id
		WHERE o.token::text=$1 AND c.household_id=$2 AND c.telegram_user_id=$3 AND c.telegram_chat_id=$4
		  AND c.status='PENDING' AND c.expires_at>now()
		LIMIT 1`, token, householdID, update.Message.From.ID, update.Message.Chat.ID).Scan(&choiceID, &field, &value, &reviewRequestID, &cardMessageID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && cardMessageID == 0) {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, cardChoiceStaleMessage)
	}
	if err != nil {
		return true, err
	}
	binding, err := p.exactAgentReviewBinding(ctx, householdID, update.Message.Chat.ID, cardMessageID)
	if err != nil {
		return true, err
	}
	if binding == nil || binding.ReviewRequestID != reviewRequestID {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, "Kartu itu sudah selesai atau berubah, jadi belum ada yang diubah.")
	}
	claimed, err := p.pool.Exec(ctx, `UPDATE telegram_pending_card_choice SET status='RESOLVED',resolved_at=now() WHERE id=$1 AND status='PENDING' AND expires_at>now()`, choiceID)
	if err != nil {
		return true, err
	}
	if claimed.RowsAffected() != 1 {
		return true, p.finishWithoutTransaction(ctx, sourceEventID, "IGNORED", update, cardChoiceStaleMessage)
	}
	applied := false
	defer func() {
		if !applied {
			_, _ = p.pool.Exec(context.WithoutCancel(ctx), `UPDATE telegram_pending_card_choice SET status='PENDING',resolved_at=NULL WHERE id=$1 AND status='RESOLVED'`, choiceID)
		}
	}()
	if engineOwnedType(binding.ReviewType) && field == string(cardAnswerCategory) {
		asTyped := update
		asTyped.CallbackQuery = nil
		card := interactionCard{ReviewRequestID: binding.ReviewRequestID, ReviewItemID: binding.ReviewItemID, ReviewType: binding.ReviewType, ChatID: update.Message.Chat.ID, MessageID: cardMessageID}
		handled, err := p.runStepEngine(ctx, householdID, sourceEventID, asTyped, card, engineInput{CategorySlug: value, Targeted: true})
		if err != nil {
			return true, err
		}
		if handled {
			applied = true
			tx, err := p.pool.Begin(ctx)
			if err != nil {
				return true, err
			}
			defer tx.Rollback(ctx)
			if err := enqueueRetireButtons(ctx, tx, update, "Sudah dipilih."); err != nil {
				return true, err
			}
			return true, tx.Commit(ctx)
		}
	}
	categories, err := p.categorySlugs(ctx, householdID)
	if err != nil {
		return true, err
	}
	bound := update
	bound.Message.ReplyToMessage = &struct {
		MessageID int64 `json:"message_id"`
	}{MessageID: cardMessageID}
	state := &agentState{SourceEventID: sourceEventID, HouseholdID: householdID, Update: bound, Now: p.now().In(jakartaLocation()),
		Categories: categories, ReviewBinding: binding, ReviewBindingCount: 1, ReviewType: binding.ReviewType, ReviewMode: binding.Kind}
	args := map[string]any{"action": "CONFIRM", field: value}
	result, _, err := p.agentResolveBoundReview(ctx, state, gateway.ToolCall{Name: "resolve_review", CallID: "card-choice"}, args)
	if err != nil {
		return true, err
	}
	applied = result.Mutation != nil
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	if err := finalizeAgentSource(ctx, tx, householdID, sourceEventID, "telegram-agent-card-choice"); err != nil {
		return true, err
	}
	if err := enqueueReply(ctx, tx, update, agentMutationFallback(result)); err != nil {
		return true, err
	}
	if err := enqueueRetireButtons(ctx, tx, update, "Sudah dipilih."); err != nil {
		return true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return true, err
	}
	return true, nil
}
