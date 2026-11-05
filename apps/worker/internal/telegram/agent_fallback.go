package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Deterministic fallback while the AI gateway is unavailable (ADR-052
// amendment, 2026-11). Deterministic flows must keep working without the
// gateway; recording a plain expense is one. When the conversational model
// cannot be reached, a message that is exactly "<what> <amount>" ("kopi 25rb",
// "bensin 50.000", "parkir 5k") is parsed in Go — and recorded only when it
// states the purchase ("beli kopi 25rb") or names something the household
// already spent on, and only when no open workflow could own the message — and recorded as NEEDS_REVIEW
// with the normal review card and no category. It is never confirmed from the
// fallback: the household picks the category on the card. Anything else (a
// question, a date, income, several items, negation or intent such as "belum
// bayar" or "rencana beli", a bill, any doubt) is not parsed; the
// household gets the honest "AI unreachable, nothing recorded" notice.

// simpleExpense is what the fallback parser read from one message.
type simpleExpense struct {
	Amount      string
	Description string
	// PurchaseVerb is true when the message says the purchase happened
	// ("beli", "bayar", "jajan", "isi"). Without it, the words alone must be a
	// phrase the household already used for a confirmed expense.
	PurchaseVerb bool
}

const fallbackMaxAmount = 100_000_000

var (
	fallbackAmountToken = regexp.MustCompile(`^(?:rp\.?)?(\d+(?:[.,]\d+)*)(rb|ribu|k|jt|juta)?$`)
	fallbackWordToken   = regexp.MustCompile(`^[\p{L}][\p{L}&'\-]*$`)
	fallbackThousands   = regexp.MustCompile(`^\d{1,3}([.,]\d{3})+$`)
	fallbackUnits       = map[string]string{"rb": "rb", "ribu": "rb", "k": "rb", "jt": "jt", "juta": "jt"}
	// fallbackRefusedWords mark messages that are not a plain expense: income,
	// transfers, savings, debts, corrections, questions, dates and times, or
	// several items. Their presence means "do not guess".
	fallbackRefusedWords = map[string]bool{
		"gaji": true, "gajian": true, "terima": true, "diterima": true, "dapat": true, "dapet": true, "masuk": true, "pemasukan": true,
		"income": true, "bonus": true, "thr": true, "refund": true, "kembalian": true, "cashback": true,
		"transfer": true, "tf": true, "kirim": true, "nabung": true, "tabung": true, "tabungan": true, "investasi": true, "saham": true, "reksa": true, "emas": true,
		"utang": true, "hutang": true, "pinjam": true, "pinjaman": true, "cicilan": true, "kredit": true,
		"ubah": true, "ganti": true, "koreksi": true, "hapus": true, "batal": true, "batalkan": true, "salah": true, "bukan": true,
		"berapa": true, "total": true, "ringkasan": true, "laporan": true, "rekap": true, "apa": true, "kapan": true, "kenapa": true,
		"kemarin": true, "kemaren": true, "besok": true, "lusa": true, "tadi": true, "tanggal": true, "tgl": true, "jam": true, "pukul": true,
		"minggu": true, "bulan": true, "tahun": true, "pagi": true, "siang": true, "sore": true, "malam": true,
		"dan": true, "sama": true, "plus": true, "terus": true, "lalu": true,
		// Negation, intent, plans, conditions and estimates: the expense may not
		// have happened, and a category-only review cannot establish that it did.
		"belum": true, "tidak": true, "tak": true, "gak": true, "ga": true, "nggak": true, "ngga": true, "enggak": true, "engga": true, "jangan": true, "no": true, "not": true,
		"rencana": true, "rencananya": true, "mau": true, "akan": true, "nanti": true, "ingin": true, "pengen": true, "pingin": true, "pengin": true, "bakal": true, "hendak": true,
		"kalau": true, "kalo": true, "jika": true, "seandainya": true, "misal": true, "misalnya": true, "andai": true,
		"harus": true, "perlu": true, "wajib": true, "budget": true, "anggaran": true, "target": true, "estimasi": true, "kira": true, "kira-kira": true, "perkiraan": true, "sekitar": true,
		"harga": true, "saldo": true, "sisa": true, "limit": true, "nilai": true, "tarif": true, "promo": true, "diskon": true, "uang": true, "duit": true, "dompet": true,
		"tagihan": true, "invoice": true, "tunggakan": true, "jatuh": true, "tempo": true, "will": true, "plan": true, "want": true,
	}
	// fallbackPurchaseVerbs state that money was spent.
	fallbackPurchaseVerbs = map[string]bool{"beli": true, "bayar": true, "jajan": true, "isi": true, "beliin": true, "dibeli": true, "dibayar": true, "membeli": true, "membayar": true}
)

// parseSimpleExpense reads "<what> <amount>" or "<amount> <what>": one amount
// (25rb, 25 ribu, 5k, 50.000, Rp 25.000, 1,5jt, 25000), and one to four plain
// words. Anything it cannot read with certainty returns ok=false.
func parseSimpleExpense(text string) (simpleExpense, bool) {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" || len([]rune(text)) > 80 || strings.ContainsAny(text, "?\n;+/:@#") {
		return simpleExpense{}, false
	}
	fields := strings.Fields(text)
	amount := ""
	var words []string
	for index := 0; index < len(fields); index++ {
		token := fields[index]
		if token == "rp" || token == "rp." {
			continue
		}
		if fallbackAmountToken.MatchString(token) {
			if amount != "" {
				return simpleExpense{}, false
			}
			unit := ""
			if index+1 < len(fields) {
				if next, ok := fallbackUnits[fields[index+1]]; ok {
					unit = next
					index++
				}
			}
			value, ok := parseFallbackAmount(token, unit)
			if !ok {
				return simpleExpense{}, false
			}
			amount = value
			continue
		}
		if !fallbackWordToken.MatchString(token) || fallbackRefusedWords[token] {
			return simpleExpense{}, false
		}
		words = append(words, token)
	}
	if amount == "" || len(words) == 0 {
		return simpleExpense{}, false
	}
	verb := false
	for len(words) > 1 && fallbackPurchaseVerbs[words[0]] {
		words, verb = words[1:], true
	}
	for _, word := range words {
		if fallbackPurchaseVerbs[word] {
			// A purchase verb anywhere but first is not the plain shape.
			return simpleExpense{}, false
		}
	}
	if len(words) > 4 {
		return simpleExpense{}, false
	}
	return simpleExpense{Amount: amount, Description: strings.Join(words, " "), PurchaseVerb: verb}, true
}

// parseFallbackAmount turns one amount token into whole rupiah. Dots and commas
// are thousands separators unless a unit follows (1,5jt, 2.5rb). A bare number
// under 500 is refused: "kopi 25" most likely means 25 ribu, and the fallback
// never guesses.
func parseFallbackAmount(token, unit string) (string, bool) {
	match := fallbackAmountToken.FindStringSubmatch(token)
	if match == nil {
		return "", false
	}
	digits := match[1]
	if match[2] != "" {
		if unit != "" {
			return "", false
		}
		unit = fallbackUnits[match[2]]
	}
	var value *big.Rat
	switch unit {
	case "":
		// Only well-formed thousands groups: 50.000, 1.250.000 or 25000.
		if strings.ContainsAny(digits, ".,") && !fallbackThousands.MatchString(digits) {
			return "", false
		}
		plain := strings.NewReplacer(".", "", ",", "").Replace(digits)
		value, _ = new(big.Rat).SetString(plain)
		if value == nil || value.Cmp(big.NewRat(500, 1)) < 0 {
			return "", false
		}
	default:
		// With a unit, one decimal separator is a decimal point: 1,5jt, 2.5rb.
		if strings.Count(digits, ".")+strings.Count(digits, ",") > 1 {
			return "", false
		}
		decimal := strings.Replace(digits, ",", ".", 1)
		number, ok := new(big.Rat).SetString(decimal)
		if !ok {
			return "", false
		}
		multiplier := int64(1_000)
		if unit == "jt" {
			multiplier = 1_000_000
		}
		value = number.Mul(number, big.NewRat(multiplier, 1))
	}
	if !value.IsInt() || value.Sign() <= 0 || value.Cmp(big.NewRat(fallbackMaxAmount, 1)) > 0 {
		return "", false
	}
	return value.Num().String(), true
}

// recordGatewayFallback records a simple expense for review when the AI gateway
// is unavailable. It returns handled=false when the message is not a plain
// expense, so the caller fails the turn honestly instead.
func (p *Processor) recordGatewayFallback(ctx context.Context, state *agentState) (bool, error) {
	if state.SideEffects > 0 || state.ReplyQueued || fallbackBlockedByWorkflow(state) {
		return false, nil
	}
	expense, ok := parseSimpleExpense(state.Update.Message.Text)
	if !ok {
		return false, nil
	}
	if !expense.PurchaseVerb {
		known, err := p.householdSpendingPhrase(ctx, state.HouseholdID, expense.Description)
		if err != nil || !known {
			return false, err
		}
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	// Claim the event: a parallel or earlier attempt that already finalized it
	// wins, and this one records nothing. Like record_transaction, the message is
	// the source of a NEEDS_REVIEW transaction (the review lives on that
	// transaction's own item), not a parked reply.
	var claimed string
	if err := tx.QueryRow(ctx, `UPDATE source_event SET processing_status=$3::text,parser_name='telegram-agent-gateway-fallback',parser_version='1' WHERE id=$1 AND household_id=$2 AND processing_status IN ('RECEIVED','PROCESSING') RETURNING id::text`, state.SourceEventID, state.HouseholdID, "NEEDS_REVIEW").Scan(&claimed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return true, err
	}
	var userID string
	if err := tx.QueryRow(ctx, `SELECT user_id FROM telegram_identity WHERE telegram_user_id=$1 AND household_id=$2 AND active`, state.Update.Message.From.ID, state.HouseholdID).Scan(&userID); err != nil {
		return true, fmt.Errorf("re-authorize Telegram identity: %w", err)
	}
	at := state.Now.Truncate(time.Minute)
	metadata, _ := json.Marshal(map[string]any{
		"decision_source": "DETERMINISTIC_FALLBACK", "fallback_reason": "GATEWAY_UNAVAILABLE",
		"time_precision": "OBSERVED_AT_PROCESSING", "policy_version": decisionPolicyVersion,
	})
	var proposalID, transactionID string
	if err := tx.QueryRow(ctx, `INSERT INTO transaction_proposal(household_id,source_event_id,proposed_type,amount,currency,transaction_at,description,confidence,proposal_status,metadata_json)
		VALUES($1,$2,'EXPENSE',$3::numeric,'IDR',$4,$5,0,'NEEDS_REVIEW',$6::jsonb) RETURNING id`, state.HouseholdID, state.SourceEventID, expense.Amount, at, expense.Description, string(metadata)).Scan(&proposalID); err != nil {
		return true, fmt.Errorf("create fallback proposal: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO transaction(household_id,type,status,amount,currency,transaction_at,description,source_confidence,classification_confidence,created_by_user_id)
		VALUES($1,'EXPENSE','NEEDS_REVIEW',$2::numeric,'IDR',$3,$4,0,0,$5) RETURNING id`, state.HouseholdID, expense.Amount, at, expense.Description, userID).Scan(&transactionID); err != nil {
		return true, fmt.Errorf("create fallback transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transaction_evidence(transaction_id,source_event_id,evidence_type,confidence,metadata_json) VALUES($1,$2,'TELEGRAM_TEXT',0,jsonb_build_object('proposal_id',$3::uuid,'decision_source','DETERMINISTIC_FALLBACK'))`, transactionID, state.SourceEventID, proposalID); err != nil {
		return true, err
	}
	refs, err := persistAgentTransactionReferencesTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "p0r9", []string{transactionID})
	if err != nil {
		return true, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'TELEGRAM',$2,'CREATE_FROM_TELEGRAM','transaction',$3,jsonb_build_object('status','NEEDS_REVIEW','proposal_id',$4::uuid,'decision_source','DETERMINISTIC_FALLBACK','fallback_reason','GATEWAY_UNAVAILABLE'))`, state.HouseholdID, userID, transactionID, proposalID); err != nil {
		return true, err
	}
	if err := EnqueueReviewRequest(ctx, tx, transactionID, "AMBIGUOUS_CATEGORY", state.Update.Message.Chat.ID, state.Update.Message.MessageID, ReviewQuestion(expense.Amount, expense.Description)); err != nil {
		return true, err
	}
	result := agentToolResult{Tool: "deterministic_fallback", Class: agentToolSideEffect, Status: "NEEDS_REVIEW", References: refs,
		Mutation: map[string]any{"action": "TRANSACTION_RECORDED", "ref": refs[0].Ref, "type": "EXPENSE", "amount_idr": expense.Amount,
			"description": expense.Description, "transaction_at": at.In(jakartaLocation()).Format(time.RFC3339), "time_precision": "OBSERVED_AT_PROCESSING", "status": "NEEDS_REVIEW",
			"decision_source": "DETERMINISTIC_FALLBACK"},
		Review: map[string]any{"required": true, "reason": "GATEWAY_UNAVAILABLE_FALLBACK", "card_in_chat": true}}
	message := gatewayFallbackMessage(result)
	if err := enqueueReply(ctx, tx, state.Update, message); err != nil {
		return true, err
	}
	if err := insertTurnTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", result.Tool, agentToolResultPublic(result)); err != nil {
		return true, err
	}
	if err := insertTurnTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "ASSISTANT", message, "", map[string]any{"deterministic_fallback": true}); err != nil {
		return true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return true, err
	}
	state.SideEffects++
	state.ReplyQueued = true
	return true, nil
}

// fallbackBlockedByWorkflow keeps the fallback away from any message that may
// belong to an open workflow: an exact Reply, a bound review or merchant
// question, a pending correction, batch or salary choice, or evidence the turn
// is about. Those wait for the model (or the card's own buttons); a new
// transaction is never created in their place.
func fallbackBlockedByWorkflow(state *agentState) bool {
	if state.Update.Message.ReplyToMessage != nil && state.Update.Message.ReplyToMessage.MessageID != 0 {
		return true
	}
	if state.ReviewBinding != nil || state.MerchantLearningBinding != nil || state.HasPendingAction || state.HasPendingBatch || state.HasSalaryChoice {
		return true
	}
	return state.TurnContext["bound_evidence"] != nil || state.TurnContext["recent_evidence"] != nil
}

// householdSpendingPhrase reports whether the words name something this
// household already spent on: a confirmed expense with that description or
// counterparty, a merchant of the household, or a category name. Without a
// purchase verb, that history is what makes "kopi 25rb" an expense and
// "saldo 50rb" not.
func (p *Processor) householdSpendingPhrase(ctx context.Context, householdID, phrase string) (bool, error) {
	var known bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM transaction WHERE household_id=$1 AND type='EXPENSE' AND status='CONFIRMED'
			AND (lower(btrim(description))=$2::text OR lower(btrim(counterparty_name))=$2::text))
		OR EXISTS(SELECT 1 FROM merchant WHERE household_id=$1 AND lower(regexp_replace(btrim(normalized_name),'[[:space:]]+',' ','g'))=$2::text)
		OR EXISTS(SELECT 1 FROM category WHERE household_id=$1 AND active AND lower(btrim(name))=$2::text)`, householdID, strings.ToLower(strings.TrimSpace(phrase))).Scan(&known)
	return known, err
}

// gatewayFallbackMessage says the AI is unreachable, what was recorded, that it
// is not final, and what the household does next.
func gatewayFallbackMessage(result agentToolResult) string {
	m := result.Mutation
	amount, _ := m["amount_idr"].(string)
	label, _ := m["description"].(string)
	when := ""
	if at, err := time.Parse(time.RFC3339, fmt.Sprint(m["transaction_at"])); err == nil {
		when = " (" + friendlyWhen(at, "", "") + ")"
	}
	return "Asisten AI Richmod sedang tidak bisa dihubungi, jadi aku catat dulu tanpa kategori: " + label + " · Rp" + FormatIDR(amount) + when +
		".\nBelum final — pilih kategorinya di kartu di bawah, ya."
}
