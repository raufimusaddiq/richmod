package jago

import (
	"fmt"
	"html"
	"io"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"

	xhtml "golang.org/x/net/html"
)

const (
	FamilyMerchantPayment  = "MERCHANT_PAYMENT"
	FamilyDebitCard        = "DEBIT_CARD"
	FamilyOutgoingTransfer = "OUTGOING_TRANSFER"
	FamilyIncomingMoney    = "INCOMING_MONEY"
	FamilyPocketMovement   = "POCKET_MOVEMENT"
	FamilyRDNMovement      = "RDN_MOVEMENT"

	EffectExpenseCandidate = "EXPENSE_CANDIDATE"
	EffectNeedsReview      = "NEEDS_REVIEW"
	EffectIgnore           = "IGNORE"
)

type ParsedEmail struct {
	MessageID             string
	Mailbox               string
	FromDomain            string
	Subject               string
	HTMLBody              string
	AuthenticationResults string
}

type Event struct {
	Family             string
	FinancialEffect    string
	Amount             string
	Currency           string
	TransactionAt      time.Time
	FromAccount        string
	ToName             string
	Merchant           string
	Reference          string
	Status             string
	TransactionChannel string
}

type Parser struct {
	expectedDomain  string
	expectedMailbox string
}

func NewParser(expectedDomain, expectedMailbox string) *Parser {
	return &Parser{expectedDomain: strings.ToLower(strings.TrimSpace(expectedDomain)), expectedMailbox: strings.ToLower(strings.TrimSpace(expectedMailbox))}
}

func (p *Parser) Name() string    { return "jago-v1" }
func (p *Parser) Version() string { return "1" }

func (p *Parser) CanParse(message ParsedEmail) bool {
	if p.expectedDomain == "" || !strings.EqualFold(strings.TrimSpace(message.FromDomain), p.expectedDomain) {
		return false
	}
	if p.expectedMailbox != "" && !strings.EqualFold(strings.TrimSpace(message.Mailbox), p.expectedMailbox) {
		return false
	}
	auth := strings.ToLower(message.AuthenticationResults)
	if !strings.Contains(auth, "dkim=pass") || !strings.Contains(auth, "dmarc=pass") {
		return false
	}
	return subjectFamily(message.Subject) != ""
}

func (p *Parser) Parse(message ParsedEmail) (Event, error) {
	if !p.CanParse(message) {
		return Event{}, fmt.Errorf("email is not a trusted known Jago template")
	}
	family := subjectFamily(message.Subject)
	event := Event{Family: family, Currency: "IDR"}
	switch family {
	case FamilyMerchantPayment:
		event.FinancialEffect, event.TransactionChannel = EffectExpenseCandidate, "MERCHANT"
	case FamilyDebitCard:
		event.FinancialEffect, event.TransactionChannel = EffectExpenseCandidate, "DEBIT_CARD"
	case FamilyOutgoingTransfer:
		event.FinancialEffect, event.TransactionChannel = EffectNeedsReview, "TRANSFER"
	case FamilyIncomingMoney:
		event.FinancialEffect, event.TransactionChannel = EffectIgnore, "INCOMING"
	case FamilyPocketMovement:
		event.FinancialEffect, event.TransactionChannel = EffectIgnore, "INTERNAL_TRANSFER"
	case FamilyRDNMovement:
		event.FinancialEffect, event.TransactionChannel = EffectIgnore, "RDN"
	}

	fields, err := semanticFields(message.HTMLBody)
	if err != nil {
		return Event{}, err
	}
	event.FromAccount = first(fields, "dari", "kantong asal", "rekening sumber")
	event.ToName = first(fields, "ke", "penerima", "tujuan")
	event.Merchant = first(fields, "merchant", "nama merchant")
	if event.Merchant == "" && family == FamilyMerchantPayment {
		event.Merchant = subjectMerchant(message.Subject)
	}
	event.Reference = first(fields, "nomor referensi", "referensi", "id transaksi")
	event.Status = first(fields, "status transaksi", "status")

	amountText := first(fields, "jumlah", "nominal", "total transaksi")
	if amountText != "" {
		amount, err := parseIDR(amountText)
		if err != nil {
			return Event{}, err
		}
		event.Amount = amount
	}
	dateText := first(fields, "tanggal transaksi", "waktu transaksi", "tanggal")
	if dateText != "" {
		parsed, err := parseJakartaTime(dateText)
		if err != nil {
			return Event{}, err
		}
		event.TransactionAt = parsed
	}

	if event.FinancialEffect != EffectIgnore && (event.Amount == "" || event.TransactionAt.IsZero()) {
		return Event{}, fmt.Errorf("known Jago template is missing amount or transaction time")
	}
	if family == FamilyMerchantPayment && event.Merchant == "" {
		return Event{}, fmt.Errorf("merchant payment is missing merchant")
	}
	return event, nil
}

func subjectFamily(subject string) string {
	value := normalizeSpace(strings.ToLower(subject))
	switch {
	case strings.Contains(value, "rdn"):
		return FamilyRDNMovement
	case strings.HasPrefix(value, "kamu telah membayar ke"):
		return FamilyMerchantPayment
	case strings.Contains(value, "transaksi menggunakan kartu debit jago"):
		return FamilyDebitCard
	case strings.HasPrefix(value, "kamu telah melakukan transfer"):
		return FamilyOutgoingTransfer
	case strings.HasPrefix(value, "asik, kamu telah menerima sejumlah uang"), strings.Contains(value, "telah menerima uang di kantong"):
		return FamilyIncomingMoney
	case strings.Contains(value, "memindahkan uang"), strings.Contains(value, "pemindahan dana antar kantong"):
		return FamilyPocketMovement
	default:
		return ""
	}
}

func semanticFields(body string) (map[string]string, error) {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(body))
	var lines []string
	var current strings.Builder
	flush := func() {
		value := normalizeSpace(html.UnescapeString(current.String()))
		if value != "" {
			lines = append(lines, value)
		}
		current.Reset()
	}
	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			if tokenizer.Err() != nil && tokenizer.Err() != io.EOF {
				return nil, fmt.Errorf("parse Jago HTML: %w", tokenizer.Err())
			}
			flush()
			fields := make(map[string]string)
			for index := 0; index+1 < len(lines); index++ {
				label := strings.TrimSuffix(strings.ToLower(lines[index]), ":")
				if knownLabel(label) && !knownLabel(strings.ToLower(lines[index+1])) {
					fields[label] = lines[index+1]
					index++
				}
			}
			return fields, nil
		case xhtml.TextToken:
			current.Write(tokenizer.Text())
		case xhtml.StartTagToken, xhtml.EndTagToken, xhtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "td", "th", "tr", "p", "div", "br", "li":
				flush()
			}
		}
	}
}

func knownLabel(value string) bool {
	value = strings.TrimSuffix(normalizeSpace(value), ":")
	for _, label := range []string{"dari", "kantong asal", "rekening sumber", "ke", "penerima", "tujuan", "merchant", "nama merchant", "nomor referensi", "referensi", "id transaksi", "status transaksi", "status", "jumlah", "nominal", "total transaksi", "tanggal transaksi", "waktu transaksi", "tanggal"} {
		if value == label {
			return true
		}
	}
	return false
}

func parseIDR(value string) (string, error) {
	normalized := strings.ToLower(normalizeSpace(value))
	normalized = strings.TrimSpace(strings.TrimPrefix(normalized, "rp"))
	normalized = strings.ReplaceAll(normalized, " ", "")
	if comma := strings.LastIndex(normalized, ","); comma >= 0 {
		if normalized[comma+1:] != "00" {
			return "", fmt.Errorf("Jago amount contains fractional rupiah")
		}
		normalized = normalized[:comma]
	}
	normalized = strings.ReplaceAll(normalized, ".", "")
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(normalized) {
		return "", fmt.Errorf("invalid Jago IDR amount")
	}
	amount, ok := new(big.Int).SetString(normalized, 10)
	if !ok || amount.Sign() <= 0 {
		return "", fmt.Errorf("invalid Jago IDR amount")
	}
	return amount.String(), nil
}

func parseJakartaTime(value string) (time.Time, error) {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.Time{}, err
	}
	normalized := normalizeSpace(strings.TrimSuffix(strings.TrimSpace(value), "WIB"))
	months := map[string]string{"Januari": "January", "Februari": "February", "Maret": "March", "April": "April", "Mei": "May", "Juni": "June", "Juli": "July", "Agustus": "August", "September": "September", "Oktober": "October", "November": "November", "Desember": "December"}
	for indonesia, english := range months {
		normalized = strings.ReplaceAll(normalized, indonesia, english)
	}
	for _, layout := range []string{"2 January 2006, 15:04", "02 January 2006, 15:04", "02/01/2006 15:04", "02-01-2006 15:04"} {
		if parsed, err := time.ParseInLocation(layout, normalized, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid Jago transaction time")
}

func subjectMerchant(subject string) string {
	value := strings.TrimSpace(subject)
	lower := strings.ToLower(value)
	prefix := "kamu telah membayar ke"
	if index := strings.Index(lower, prefix); index >= 0 {
		return strings.TrimFunc(strings.TrimSpace(value[index+len(prefix):]), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	}
	return ""
}

func first(fields map[string]string, names ...string) string {
	for _, name := range names {
		if value := fields[name]; value != "" {
			return value
		}
	}
	return ""
}

func normalizeSpace(value string) string { return strings.Join(strings.Fields(value), " ") }
