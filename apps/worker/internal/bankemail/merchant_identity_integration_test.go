package bankemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/testsupport"
)

func TestResolveMerchantIDTreatsCaseAndWhitespaceAsOneIdentity(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var householdID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Merchant identity %d", testsupport.UniqueStamp())).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	first, err := resolveMerchantID(ctx, tx, householdID, "Calorie Snacks & Desserts")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveMerchantID(ctx, tx, householdID, "  CALORIE   SNACKS & DESSERTS  ")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("merchant ids differ: %s != %s", first, second)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE household_id=$1`, householdID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("merchant count=%d", count)
	}
	var stored string
	if err := tx.QueryRow(ctx, `SELECT normalized_name FROM merchant WHERE household_id=$1`, householdID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "CALORIE SNACKS & DESSERTS" {
		t.Fatalf("new bank merchant name=%q", stored)
	}
}

func TestMerchantIdentityNormalizesBankAndEwalletSuffixes(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	newHousehold := func(t *testing.T, label string) string {
		var id string
		must(t, pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("%s %d", label, stamp)).Scan(&id))
		return id
	}
	newCategory := func(t *testing.T, household, name string) string {
		var id string
		slug := strings.ReplaceAll(strings.ToLower(name), " ", "-")
		must(t, pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, household, name, fmt.Sprintf("%s-%d", slug, stamp)).Scan(&id))
		return id
	}
	newMerchant := func(t *testing.T, household, name string) string {
		var id string
		must(t, pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, household, name).Scan(&id))
		return id
	}
	newAlias := func(t *testing.T, household, raw, merchant, category string) {
		must(t, func() error {
			_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,$2,$3,NULLIF($4,'')::uuid,true,true)`, household, raw, merchant, category)
			return err
		}())
	}

	t.Run("bank_suffixed_payee_resolves_to_short_alias", func(t *testing.T) {
		household := newHousehold(t, "Bank suffix")
		category := newCategory(t, household, "Transfer")
		shortMerchant := newMerchant(t, household, "PAYEE SHORT")
		newAlias(t, household, "JOHN DOE", shortMerchant, category)
		long := "JOHN DOE BANK SYARIAH INDONESIA 1128 5635 31"
		memory, err := loadMerchantMemory(ctx, pool, household, long)
		must(t, err)
		if !memory.AutoApply || memory.MerchantID != shortMerchant || memory.CategoryID != category {
			t.Fatalf("memory=%+v", memory)
		}
		tx, err := pool.Begin(ctx)
		must(t, err)
		defer tx.Rollback(ctx)
		id, err := resolveMerchantID(ctx, tx, household, long)
		must(t, err)
		if id != shortMerchant {
			t.Fatalf("resolve=%s want %s", id, shortMerchant)
		}
		var count int
		must(t, tx.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE household_id=$1`, household).Scan(&count))
		if count != 1 {
			t.Fatalf("merchant count=%d", count)
		}
	})

	t.Run("alias_precedes_leftover_merchant_row", func(t *testing.T) {
		household := newHousehold(t, "Bank alias precedence")
		category := newCategory(t, household, "Precedence")
		target := newMerchant(t, household, "PAYEE TARGET")
		long := "PAYEE TARGET BANK MANDIRI 1128 5635 31"
		newMerchant(t, household, long)
		newAlias(t, household, "PAYEE TARGET", target, category)
		memory, err := loadMerchantMemory(ctx, pool, household, long)
		must(t, err)
		if memory.MerchantID != target || memory.CategoryID != category {
			t.Fatalf("alias did not win over leftover merchant row: %+v", memory)
		}
		tx, err := pool.Begin(ctx)
		must(t, err)
		defer tx.Rollback(ctx)
		id, err := resolveMerchantID(ctx, tx, household, long)
		must(t, err)
		if id != target {
			t.Fatalf("resolve=%s want alias target %s", id, target)
		}
		review, err := reviewdomain.MerchantLinkNeedsReview(ctx, tx, household, long)
		must(t, err)
		if review {
			t.Fatal("a single alias hit must not need review because a leftover merchant row matches")
		}
	})

	t.Run("exact_alias_precedes_normalized_alias", func(t *testing.T) {
		household := newHousehold(t, "Bank ambiguous")
		shortCategory := newCategory(t, household, "Ambiguous A")
		longCategory := newCategory(t, household, "Ambiguous B")
		shortMerchant := newMerchant(t, household, "PAYEE SHORT")
		longMerchant := newMerchant(t, household, "PAYEE LONG")
		long := "JOHN DOE BANK MANDIRI 1128 5635 31"
		newAlias(t, household, "JOHN DOE", shortMerchant, shortCategory)
		newAlias(t, household, long, longMerchant, longCategory)
		memory, err := loadMerchantMemory(ctx, pool, household, long)
		must(t, err)
		if memory.MerchantID != longMerchant || memory.CategoryID != longCategory {
			t.Fatalf("exact alias did not win: %+v", memory)
		}
		tx, err := pool.Begin(ctx)
		must(t, err)
		defer tx.Rollback(ctx)
		id, err := resolveMerchantID(ctx, tx, household, long)
		must(t, err)
		if id != longMerchant {
			t.Fatalf("exact alias resolve=%s want %s", id, longMerchant)
		}
		var count int
		must(t, tx.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE household_id=$1`, household).Scan(&count))
		if count != 2 {
			t.Fatalf("merchant count=%d", count)
		}
	})

	t.Run("digits_only_without_alias_is_not_linked", func(t *testing.T) {
		household := newHousehold(t, "Bank empty")
		newMerchant(t, household, "DIGITS TARGET")
		tx, err := pool.Begin(ctx)
		must(t, err)
		defer tx.Rollback(ctx)
		for _, raw := range []string{"1128 5635 31", "  1128   5635   31  "} {
			memory, err := loadMerchantMemory(ctx, pool, household, raw)
			must(t, err)
			if memory.MerchantID != "" || memory.CategoryID != "" {
				t.Fatalf("empty normalized form %q linked: %+v", raw, memory)
			}
			id, err := resolveMerchantID(ctx, tx, household, raw)
			must(t, err)
			if id != "" {
				t.Fatalf("empty normalized form %q resolved to %s", raw, id)
			}
		}
		var count int
		must(t, tx.QueryRow(ctx, `SELECT count(*) FROM merchant WHERE household_id=$1`, household).Scan(&count))
		if count != 1 {
			t.Fatalf("merchant count=%d", count)
		}
	})

	t.Run("single_token_institution_remnant_is_not_linked", func(t *testing.T) {
		household := newHousehold(t, "Bank institution")
		category := newCategory(t, household, "Sari")
		shortMerchant := newMerchant(t, household, "SARI")
		newAlias(t, household, "SARI", shortMerchant, category)
		tx, err := pool.Begin(ctx)
		must(t, err)
		defer tx.Rollback(ctx)
		id, err := resolveMerchantID(ctx, tx, household, "SARI PERMATA")
		must(t, err)
		if id == shortMerchant {
			t.Fatal("an institution word without an account tail must not strip to the existing merchant")
		}
		id, err = resolveMerchantID(ctx, tx, household, "SARI PERMATA 1234567890")
		must(t, err)
		if id == shortMerchant {
			t.Fatal("a one-token institution remnant must not link to the existing merchant")
		}
		id, err = resolveMerchantID(ctx, tx, household, "SARI 1234567890")
		must(t, err)
		if id != shortMerchant {
			t.Fatalf("account-tail-only one-token name resolved=%s want %s", id, shortMerchant)
		}
	})

	t.Run("household_isolation", func(t *testing.T) {
		own := newHousehold(t, "Bank isolation own")
		other := newHousehold(t, "Bank isolation other")
		category := newCategory(t, other, "Isolation")
		merchant := newMerchant(t, other, "OTHER HOUSEHOLD PAYEE")
		newAlias(t, other, "JANE DOE", merchant, category)
		memory, err := loadMerchantMemory(ctx, pool, own, "JANE DOE BANK BCA 1128 5635 31")
		must(t, err)
		if memory.MerchantID != "" || memory.CategoryID != "" {
			t.Fatalf("cross-household memory=%+v", memory)
		}
	})
}

func TestBankEmailAmbiguousMerchantRoutesToUnknownMerchantReview(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, shortCategory, longCategory, listenerID, sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank merchant review %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-merchant-%d@test.invalid", stamp)).Scan(&owner))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, owner)
		return err
	}())
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id) VALUES($1,$2,$3)`, stamp, household, owner)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&shortCategory))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Other','other') RETURNING id`, household).Scan(&longCategory))
	var shortMerchant, longMerchant string
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, household).Scan(&shortMerchant))
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE LONG') RETURNING id`, household).Scan(&longMerchant))
	long := "1128 5635 31"
	aliasRaw := "JOHN DOE BANK MANDIRI 1128 5635 31"
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'JOHN DOE',$2,$3,true,true),($1,$4,$5,$6,true,true)`, household, shortMerchant, shortCategory, aliasRaw, longMerchant, longCategory)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
		return err
	}())
	amount, direction, channel, description := "87931", "OUTGOING", "DEBIT_CARD", "Pembayaran"
	at := time.Now().Add(-time.Hour).UTC()
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &long, Description: &description, TransactionAt: &at, Confidence: .95}
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: shortCategory}
	must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))

	var transactionID, status string
	var merchantID *string
	must(pool.QueryRow(ctx, `SELECT t.id::text,t.status,t.merchant_id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, sourceID).Scan(&transactionID, &status, &merchantID))
	if status != "NEEDS_REVIEW" || merchantID != nil {
		t.Fatalf("ambiguous merchant was not parked: status=%s merchant=%v", status, merchantID)
	}
	var reviewType string
	must(pool.QueryRow(ctx, `SELECT review_type FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND status IN ('OPEN','PENDING_SEND')`, household, transactionID).Scan(&reviewType))
	if reviewType != "UNKNOWN_MERCHANT" {
		t.Fatalf("review type=%s", reviewType)
	}
	var itemID, transactionCategory string
	var rawDecision []byte
	must(pool.QueryRow(ctx, `SELECT ri.id::text,t.category_id::text,ri.decision FROM review_item ri JOIN transaction t ON t.id=ri.transaction_id WHERE ri.household_id=$1 AND ri.transaction_id=$2 AND ri.status IN ('OPEN','PENDING_SEND')`, household, transactionID).Scan(&itemID, &transactionCategory, &rawDecision))
	var decision struct {
		MissingFacts []string       `json:"missingFacts"`
		KnownFacts   map[string]any `json:"knownFacts"`
	}
	must(json.Unmarshal(rawDecision, &decision))
	if len(decision.MissingFacts) != 1 || decision.MissingFacts[0] != "merchant" {
		t.Fatalf("ambiguous merchant missing facts=%v", decision.MissingFacts)
	}
	if _, known := decision.KnownFacts["merchant"]; known {
		t.Fatalf("ambiguous merchant must not be known: %v", decision.KnownFacts)
	}
	if decision.KnownFacts["category_id"] != shortCategory || transactionCategory != shortCategory {
		t.Fatalf("accepted category lost: known=%v transaction=%s want %s", decision.KnownFacts["category_id"], transactionCategory, shortCategory)
	}
	var noticeAt *time.Time
	must(pool.QueryRow(ctx, `SELECT notice_at FROM bank_email_extraction WHERE source_event_id=$1`, sourceID).Scan(&noticeAt))
	if noticeAt != nil {
		t.Fatalf("ambiguous merchant claimed notice_at: %v", noticeAt)
	}
	var notices int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM job WHERE type='SEND_TELEGRAM_MESSAGE' AND (payload_json->>'chat_id')::bigint=$1 AND payload_json->>'text' LIKE '%Tersimpan%'`, stamp).Scan(&notices))
	if notices != 0 {
		t.Fatalf("ambiguous merchant enqueued %d saved notices", notices)
	}
	tx, err := pool.Begin(ctx)
	must(err)
	defer tx.Rollback(ctx)
	_, err = reviewdomain.ConfirmTransactionReview(ctx, tx, reviewdomain.ConfirmCommand{
		HouseholdID: household, ActorUserID: owner, TransactionID: transactionID, ReviewItemID: itemID,
		Action: "CONFIRM_REVIEW", MerchantName: "JOHN DOE", ResolveReview: true,
	})
	must(err)
	must(tx.Commit(ctx))
	var confirmedStatus, confirmedCategory, confirmedMerchant string
	must(pool.QueryRow(ctx, `SELECT status,category_id::text,merchant_id::text FROM transaction WHERE id=$1`, transactionID).Scan(&confirmedStatus, &confirmedCategory, &confirmedMerchant))
	if confirmedStatus != "CONFIRMED" || confirmedCategory != shortCategory || confirmedMerchant != shortMerchant {
		t.Fatalf("merchant-only confirmation status=%s category=%s merchant=%s", confirmedStatus, confirmedCategory, confirmedMerchant)
	}
}

func TestBankEmailAmbiguousMerchantWithMissingCategoryAsksForBoth(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, category, listenerID, sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank both missing %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-both-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
	var shortMerchant, longMerchant string
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, household).Scan(&shortMerchant))
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE LONG') RETURNING id`, household).Scan(&longMerchant))
	long := "1128 5635 31"
	aliasRaw := "JOHN DOE BANK MANDIRI 1128 5635 31"
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'JOHN DOE',$2),($1,$3,$4)`, household, shortMerchant, aliasRaw, longMerchant)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
		return err
	}())
	amount, direction, channel, description := "73119", "OUTGOING", "DEBIT_CARD", "Pembayaran"
	at := time.Now().Add(-time.Hour).UTC()
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &long, Description: &description, TransactionAt: &at, Confidence: .95}
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := PolicyResult{Type: "EXPENSE", Status: "NEEDS_REVIEW", ReviewType: "AMBIGUOUS_CATEGORY"}
	must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))

	var transactionID, status string
	var merchantID, transactionCategory *string
	must(pool.QueryRow(ctx, `SELECT t.id::text,t.status,t.merchant_id::text,t.category_id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, sourceID).Scan(&transactionID, &status, &merchantID, &transactionCategory))
	if status != "NEEDS_REVIEW" || merchantID != nil || transactionCategory != nil {
		t.Fatalf("transaction status=%s merchant=%v category=%v", status, merchantID, transactionCategory)
	}
	var itemID string
	var rawDecision []byte
	must(pool.QueryRow(ctx, `SELECT id::text,decision FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND status IN ('OPEN','PENDING_SEND')`, household, transactionID).Scan(&itemID, &rawDecision))
	var decision struct {
		MissingFacts []string       `json:"missingFacts"`
		KnownFacts   map[string]any `json:"knownFacts"`
	}
	must(json.Unmarshal(rawDecision, &decision))
	missing := map[string]bool{}
	for _, fact := range decision.MissingFacts {
		missing[fact] = true
	}
	if !missing["merchant"] || !missing["category"] {
		t.Fatalf("missing facts=%v", decision.MissingFacts)
	}
	if _, known := decision.KnownFacts["merchant"]; known {
		t.Fatalf("ambiguous merchant must not be known: %v", decision.KnownFacts)
	}
	tx, err := pool.Begin(ctx)
	must(err)
	defer tx.Rollback(ctx)
	_, err = reviewdomain.ConfirmTransactionReview(ctx, tx, reviewdomain.ConfirmCommand{
		HouseholdID: household, ActorUserID: owner, TransactionID: transactionID, ReviewItemID: itemID,
		Action: "CONFIRM_REVIEW", CategorySupplied: true, CategoryID: category, MerchantName: "JOHN DOE", ResolveReview: true,
	})
	must(err)
	must(tx.Commit(ctx))
	var confirmedStatus, confirmedCategory, confirmedMerchant string
	must(pool.QueryRow(ctx, `SELECT status,category_id::text,merchant_id::text FROM transaction WHERE id=$1`, transactionID).Scan(&confirmedStatus, &confirmedCategory, &confirmedMerchant))
	if confirmedStatus != "CONFIRMED" || confirmedCategory != category || confirmedMerchant != shortMerchant {
		t.Fatalf("confirmation status=%s category=%s merchant=%s", confirmedStatus, confirmedCategory, confirmedMerchant)
	}
}

func TestBankEmailInstitutionFallbackNeverLinksSingleTokenMerchant(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, category, listenerID, sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank single token %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-single-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
	var sari string
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'SARI') RETURNING id`, household).Scan(&sari))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
		return err
	}())
	amount, direction, channel, merchant, description := "64217", "OUTGOING", "DEBIT_CARD", "SARI PERMATA 1234567890", "Pembayaran"
	at := time.Now().Add(-time.Hour).UTC()
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &merchant, Description: &description, TransactionAt: &at, Confidence: .95}
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: category}
	must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))
	var transactionID, status string
	var merchantID *string
	must(pool.QueryRow(ctx, `SELECT t.id::text,t.status,t.merchant_id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, sourceID).Scan(&transactionID, &status, &merchantID))
	if status != "CONFIRMED" || merchantID == nil || *merchantID == sari {
		t.Fatalf("institution fallback linked the single-token merchant: status=%s merchant=%v", status, merchantID)
	}
	var reviews int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND status IN ('OPEN','PENDING_SEND')`, household, transactionID).Scan(&reviews))
	if reviews != 0 {
		t.Fatalf("resolvable fallback payee opened %d reviews", reviews)
	}
}

func TestMerchantIdentityIgnoresCrossHouseholdAliasTarget(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := testsupport.UniqueStamp()
	var own, foreign, category, foreignMerchant string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Tenant own %d", stamp)).Scan(&own); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Tenant foreign %d", stamp)).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, own).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, foreign).Scan(&foreignMerchant); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role='replica'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'PAYEE SHORT',$2,$3,true,true)`, own, foreignMerchant, category); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role='origin'`); err != nil {
		t.Fatal(err)
	}
	merchantID, err := reviewdomain.ExistingMerchantID(ctx, tx, own, "PAYEE SHORT")
	if err != nil {
		t.Fatal(err)
	}
	if merchantID != "" {
		t.Fatalf("cross-household alias target resolved to %s", merchantID)
	}
	memory, err := loadMerchantMemory(ctx, tx, own, "PAYEE SHORT")
	if err != nil {
		t.Fatal(err)
	}
	if memory.MerchantID != "" || memory.CategoryID != "" {
		t.Fatalf("cross-household alias target reached memory: %+v", memory)
	}
}

func TestResolveMerchantIDNormalizesLongPayeeBeforeTruncation(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := testsupport.UniqueStamp()
	var household, merchant string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Long payee %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	short := "PAYEE " + strings.Repeat("A", 140)
	if err := pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, household, short).Scan(&merchant); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	id, err := resolveMerchantID(ctx, tx, household, short+" BANK MANDIRI 1128 5635 31")
	if err != nil {
		t.Fatal(err)
	}
	if id != merchant {
		t.Fatalf("long payee resolved=%s want %s", id, merchant)
	}
}

func TestBankEmailTransferReviewUsesLegacyExactMerchantLink(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, merchant, listenerID, sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank transfer merchant %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-transfer-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE EXACT') RETURNING id`, household).Scan(&merchant))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'PAYEE EXACT',$2)`, household, merchant)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
		return err
	}())
	amount, direction, channel, payee, description := "41873", "OUTGOING", "TRANSFER", "PAYEE EXACT", "Transfer"
	at := time.Now().Add(-time.Hour).UTC()
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &payee, Description: &description, TransactionAt: &at, Confidence: .95}
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := EvaluateBankEmail(listener, extraction, nil)
	if result.ReviewType != "TRANSFER_CLASSIFICATION" {
		t.Fatalf("fixture review type=%s", result.ReviewType)
	}
	must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))
	var transactionID, reviewType, missing string
	var merchantID *string
	must(pool.QueryRow(ctx, `SELECT t.id::text,t.merchant_id::text,ri.review_type,COALESCE(ri.decision->'missingFacts'->>0,'') FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id JOIN review_item ri ON ri.transaction_id=t.id WHERE e.source_event_id=$1`, sourceID).Scan(&transactionID, &merchantID, &reviewType, &missing))
	if merchantID == nil || *merchantID != merchant || reviewType != "TRANSFER_CLASSIFICATION" || missing != "transfer_relationship" {
		t.Fatalf("transaction=%s merchant=%v review=%s missing=%s", transactionID, merchantID, reviewType, missing)
	}
}

func TestBankEmailDuplicateAmbiguousMerchantKeepsMerchantGap(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, category, listenerID, sourceID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank duplicate merchant %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-dup-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
	var shortMerchant, longMerchant string
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE SHORT') RETURNING id`, household).Scan(&shortMerchant))
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'PAYEE LONG') RETURNING id`, household).Scan(&longMerchant))
	long := "1128 5635 31"
	aliasRaw := "JOHN DOE BANK MANDIRI 1128 5635 31"
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id) VALUES($1,'JOHN DOE',$2),($1,$3,$4)`, household, shortMerchant, aliasRaw, longMerchant)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
		return err
	}())
	at := time.Now().Add(-time.Hour).UTC()
	_, err = pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at,description,confirmed_at) VALUES($1,'EXPENSE','CONFIRMED',87931,$2,'Belanja lain',now())`, household, at.Add(-15*time.Minute))
	must(err)
	amount, direction, channel, description := "87931", "OUTGOING", "DEBIT_CARD", "Pembayaran"
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &long, Description: &description, TransactionAt: &at, Confidence: .95}
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := EvaluateBankEmail(listener, extraction, nil)
	must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))
	var transactionID string
	var merchantID *string
	must(pool.QueryRow(ctx, `SELECT t.id::text,t.merchant_id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, sourceID).Scan(&transactionID, &merchantID))
	if merchantID != nil {
		t.Fatalf("duplicate ambiguous merchant transaction merchant=%v", merchantID)
	}
	var reviewType string
	var duplicateGap, merchantGap, knownMerchant bool
	must(pool.QueryRow(ctx, `SELECT review_type,decision->'missingFacts' ? 'duplicate_relationship',decision->'missingFacts' ? 'merchant',decision->'knownFacts' ? 'merchant' FROM review_item WHERE household_id=$1 AND transaction_id=$2 AND status IN ('OPEN','PENDING_SEND')`, household, transactionID).Scan(&reviewType, &duplicateGap, &merchantGap, &knownMerchant))
	if reviewType != "POSSIBLE_DUPLICATE" || !duplicateGap || !merchantGap || knownMerchant {
		t.Fatalf("review type=%s duplicateGap=%t merchantGap=%t knownMerchant=%t", reviewType, duplicateGap, merchantGap, knownMerchant)
	}
}

func TestBankEmailEmptyNormalizedPayeeLearnsExactAlias(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, category, listenerID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank learned exact %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-learn-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	raw := "1128 5635 31"
	process := func(label, amount string) string {
		var sourceID string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, label, []byte(label)).Scan(&sourceID))
		must(func() error {
			_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
			return err
		}())
		direction, channel, description := "OUTGOING", "DEBIT_CARD", "Pembayaran"
		at := time.Now().Add(-time.Hour).UTC()
		extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &raw, Description: &description, TransactionAt: &at, Confidence: .95}
		result := PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: category}
		must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))
		return sourceID
	}
	first := process(fmt.Sprintf("learn-%d-1", stamp), "66111")
	var transactionID, itemID string
	must(pool.QueryRow(ctx, `SELECT t.id::text,ri.id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id JOIN review_item ri ON ri.transaction_id=t.id WHERE e.source_event_id=$1 AND ri.status IN ('OPEN','PENDING_SEND')`, first).Scan(&transactionID, &itemID))
	tx, err := pool.Begin(ctx)
	must(err)
	_, err = reviewdomain.ConfirmTransactionReview(ctx, tx, reviewdomain.ConfirmCommand{
		HouseholdID: household, ActorUserID: owner, TransactionID: transactionID, ReviewItemID: itemID,
		Action: "CONFIRM_REVIEW", CategorySupplied: true, CategoryID: category, MerchantName: "SARI PERMATA", RememberMerchant: true, ResolveReview: true,
	})
	must(err)
	must(tx.Commit(ctx))
	var learnedAlias int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM merchant_alias WHERE household_id=$1 AND lower(regexp_replace(btrim(raw_name),'[[:space:]]+',' ','g'))=lower($2)`, household, raw).Scan(&learnedAlias))
	if learnedAlias != 1 {
		t.Fatalf("confirm did not write the raw alias: %d", learnedAlias)
	}
	second := process(fmt.Sprintf("learn-%d-2", stamp), "66112")
	var status string
	var merchantID *string
	must(pool.QueryRow(ctx, `SELECT t.status,t.merchant_id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, second).Scan(&status, &merchantID))
	if status != "CONFIRMED" || merchantID == nil {
		t.Fatalf("learned exact alias did not link: status=%s merchant=%v", status, merchantID)
	}
	var open int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM review_item WHERE household_id=$1 AND transaction_id=(SELECT transaction_id FROM transaction_evidence WHERE source_event_id=$2) AND status IN ('OPEN','PENDING_SEND')`, household, second).Scan(&open))
	if open != 0 {
		t.Fatalf("learned exact alias still opened %d review items", open)
	}
}

func TestBankEmailDigitsOnlyExactAliasLinks(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp := testsupport.UniqueStamp()
	var household, owner, account, category, listenerID, sourceID, merchant string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Bank digits %d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("bank-digits-%d@test.invalid", stamp)).Scan(&owner))
	must(pool.QueryRow(ctx, `INSERT INTO account(household_id,name,account_type,tracking_policy) VALUES($1,'Bank','BANK','SPENDING_ONLY') RETURNING id`, household).Scan(&account))
	must(pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Food','food') RETURNING id`, household).Scan(&category))
	must(pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,'DIGITS PAYEE') RETURNING id`, household).Scan(&merchant))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'1128 5635 31',$2,$3,true,true)`, household, merchant, category)
		return err
	}())
	must(pool.QueryRow(ctx, `INSERT INTO bank_email_listener(household_id,bank_name,sender_address,created_by_user_id,account_id) VALUES($1,'Bank','bank@test.invalid',$2,$3) RETURNING id`, household, owner, account).Scan(&listenerID))
	must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'BANK_EMAIL',$2,now(),$3,'RECEIVED') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&sourceID))
	must(func() error {
		_, err := pool.Exec(ctx, `INSERT INTO bank_email_extraction(source_event_id,listener_id,protocol,tool_schema_version,output_json,validation_status) VALUES($1,$2,'native_tool','1','{}','VALID')`, sourceID, listenerID)
		return err
	}())
	amount, direction, channel, payee, description := "51234", "OUTGOING", "DEBIT_CARD", "1128 5635 31", "Pembayaran"
	at := time.Now().Add(-time.Hour).UTC()
	extraction := Extraction{Kind: "TRANSACTION", AmountIDR: &amount, Direction: &direction, Channel: &channel, Merchant: &payee, Description: &description, TransactionAt: &at, Confidence: .95}
	listener := Listener{ID: listenerID, HouseholdID: household, BankName: "Bank", SenderAddress: "bank@test.invalid", AccountID: account, TrackingPolicy: "SPENDING_ONLY", Active: true}
	result := PolicyResult{Type: "EXPENSE", Status: "CONFIRMED", AutoConfirm: true, CategoryID: category}
	must((&Processor{pool: pool}).persist(ctx, listener, sourceID, extraction, result, ""))
	var status string
	var merchantID *string
	must(pool.QueryRow(ctx, `SELECT t.status,t.merchant_id::text FROM transaction t JOIN transaction_evidence e ON e.transaction_id=t.id WHERE e.source_event_id=$1`, sourceID).Scan(&status, &merchantID))
	if status != "CONFIRMED" || merchantID == nil || *merchantID != merchant {
		t.Fatalf("digits exact alias status=%s merchant=%v want %s", status, merchantID, merchant)
	}
}

func TestLoadMerchantMemoryRequiresOneUnambiguousNormalizedRule(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var householdID, categoryID, merchantID string
	stamp := testsupport.UniqueStamp()
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Merchant memory %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT id FROM category WHERE household_id=$1 AND active LIMIT 1`, householdID).Scan(&categoryID); err == nil {
		t.Fatal("new household unexpectedly has a category")
	}
	if err = pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,$2,$3) RETURNING id`, householdID, "Food", fmt.Sprintf("food-%d", stamp)).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO merchant(household_id,normalized_name) VALUES($1,$2) RETURNING id`, householdID, "Cafe").Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,'Cafe',$2,$3,true,true)`, householdID, merchantID, categoryID); err != nil {
		t.Fatal(err)
	}
	memory, err := loadMerchantMemory(ctx, pool, householdID, " cafe ")
	if err != nil || !memory.AutoApply || memory.CategoryID != categoryID {
		t.Fatalf("expected normalized learned match: %+v, %v", memory, err)
	}
	var otherHousehold string
	if err = pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Other merchant household %d", stamp)).Scan(&otherHousehold); err != nil {
		t.Fatal(err)
	}
	if other, err := loadMerchantMemory(ctx, pool, otherHousehold, "Cafe"); err != nil || other.CategoryID != "" {
		t.Fatalf("cross-household alias leaked: %+v, %v", other, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE category SET active=false WHERE id=$1`, categoryID); err != nil {
		t.Fatal(err)
	}
	if inactive, err := loadMerchantMemory(ctx, pool, householdID, "Cafe"); err != nil || inactive.CategoryID != "" {
		t.Fatalf("inactive category reused: %+v, %v", inactive, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO merchant_alias(household_id,raw_name,normalized_merchant_id,default_category_id,auto_apply,created_from_user_confirmation) VALUES($1,' CAFE  ',$2,$3,true,true)`, householdID, merchantID, categoryID); err == nil {
		t.Fatal("normalized duplicate alias was accepted")
	}
}
