package reviewdomain

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type lifecycleFixture struct {
	t               *testing.T
	ctx             context.Context
	pool            *pgxpool.Pool
	household, user string
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &lifecycleFixture{t: t, ctx: ctx, pool: pool}
	stamp := time.Now().UnixNano()
	f.must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Lifecycle followups %d", stamp)).Scan(&f.household))
	f.must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("lifecycle-%d@test.invalid", stamp)).Scan(&f.user))
	return f
}

func (f *lifecycleFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *lifecycleFixture) id(sql string, args ...any) string {
	f.t.Helper()
	var id string
	f.must(f.pool.QueryRow(f.ctx, sql, args...).Scan(&id))
	return id
}

func (f *lifecycleFixture) source(kind string) string {
	f.t.Helper()
	stamp := fmt.Sprint(time.Now().UnixNano())
	return f.id(`INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,$2,$3,now(),$4,'NEEDS_REVIEW') RETURNING id`, f.household, kind, stamp, []byte(stamp))
}

func (f *lifecycleFixture) status(source string) string {
	f.t.Helper()
	var status string
	f.must(f.pool.QueryRow(f.ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status))
	return status
}

func (f *lifecycleFixture) inTx(run func(tx pgx.Tx) error) {
	f.t.Helper()
	tx, err := f.pool.Begin(f.ctx)
	f.must(err)
	defer tx.Rollback(f.ctx)
	f.must(run(tx))
	f.must(tx.Commit(f.ctx))
}

// A dismissed wealth observation is rejected evidence whatever its origin: a
// provider-email one settles its source exactly like a document one does.
func TestDismissedWealthObservationSettlesBothOrigins(t *testing.T) {
	f := newLifecycleFixture(t)
	decision := `{"version":1,"reasonCode":"WEALTH_OBSERVATION_CONFIRMATION","knownFacts":{},"missingFacts":["wealth_snapshot_confirmation"],"allowedActions":["SET_WEALTH_ACCOUNT","IGNORE"]}`
	// Document origin.
	documentSource := f.source("TELEGRAM_IMAGE")
	attachment := f.id(`INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, f.household, []byte(documentSource), f.household+"/"+documentSource+".png")
	document := f.id(`INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'WEALTH_OBSERVATION','NEEDS_REVIEW') RETURNING id`, f.household, documentSource, attachment)
	documentObservation := f.id(`INSERT INTO wealth_observation(household_id,document_id,institution,account_hint,observed_value_idr) VALUES($1,$2,'Bibit','Reksadana',1000) RETURNING id`, f.household, document)
	documentItem := f.id(`INSERT INTO review_item(household_id,wealth_observation_id,review_type,status,decision) VALUES($1,$2,'WEALTH_OBSERVATION_CONFIRMATION','OPEN',$3::jsonb) RETURNING id`, f.household, documentObservation, decision)
	// Provider-email origin: the review binds both observations (migration 00052).
	emailSource := f.source("FINANCIAL_EMAIL")
	emailObservation := f.id(`INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,0,'WEALTH_VALUE','{}'::jsonb,'REVIEW') RETURNING id`, f.household, emailSource)
	emailWealth := f.id(`INSERT INTO wealth_observation(household_id,financial_email_observation_id,institution,account_hint,observed_value_idr) VALUES($1,$2,'Bibit','Reksadana',1000) RETURNING id`, f.household, emailObservation)
	emailItem := f.id(`INSERT INTO review_item(household_id,wealth_observation_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,$3,'WEALTH_OBSERVATION_CONFIRMATION','OPEN',$4::jsonb) RETURNING id`, f.household, emailWealth, emailObservation, decision)
	for _, item := range []string{documentItem, emailItem} {
		f.inTx(func(tx pgx.Tx) error {
			return IgnoreReviewItem(f.ctx, tx, IgnoreCommand{HouseholdID: f.household, ActorUserID: f.user, ReviewItemID: item})
		})
	}
	if got, want := f.status(emailSource), f.status(documentSource); got != "IGNORED" || want != "IGNORED" {
		t.Fatalf("a dismissed wealth observation must settle both origins alike: email=%s document=%s", got, want)
	}
	// An accepted sibling still wins over the dismissal on a provider email.
	f.must(f.pool.QueryRow(f.ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,1,'CASH_MOVEMENT','{}'::jsonb,'APPLIED') RETURNING id`, f.household, emailSource).Scan(new(string)))
	f.inTx(func(tx pgx.Tx) error { return RefreshSourceLifecycle(f.ctx, tx, f.household, emailSource) })
	if got := f.status(emailSource); got != "PROCESSED" {
		t.Fatalf("accepted provider evidence must win over a dismissed wealth value: %s", got)
	}
}

// Reopening a dismissed source- or document-bound review leaves the status to
// the shared refresh: the new open item is what puts the source back in review.
func TestReopenedSourceReviewIsSettledByTheSharedRefresh(t *testing.T) {
	f := newLifecycleFixture(t)
	for _, bound := range []string{"source_event", "document"} {
		t.Run(bound, func(t *testing.T) {
			source := f.source("TELEGRAM_IMAGE")
			column, subject, kind := "source_event_id", source, "UNKNOWN_BANK_TEMPLATE"
			if bound == "document" {
				attachment := f.id(`INSERT INTO attachment(household_id,content_hash,media_type,byte_size,width,height,storage_ref) VALUES($1,$2,'image/png',100,10,10,$3) RETURNING id`, f.household, []byte(source), f.household+"/"+source+".png")
				column, kind = "document_id", "DOCUMENT_CLASSIFICATION"
				subject = f.id(`INSERT INTO document(household_id,source_event_id,attachment_id,document_type,status) VALUES($1,$2,$3,'OTHER_FINANCIAL_DOCUMENT','NEEDS_REVIEW') RETURNING id`, f.household, source, attachment)
			}
			item := f.id(`INSERT INTO review_item(household_id,`+column+`,review_type,status,decision) VALUES($1,$2,$3,'OPEN',jsonb_build_object('version',1,'reasonCode',$3::text,'knownFacts','{}'::jsonb,'missingFacts','[]'::jsonb,'allowedActions','["IGNORE"]'::jsonb)) RETURNING id`, f.household, subject, kind)
			f.inTx(func(tx pgx.Tx) error {
				return IgnoreReviewItem(f.ctx, tx, IgnoreCommand{HouseholdID: f.household, ActorUserID: f.user, ReviewItemID: item})
			})
			if got := f.status(source); got != "IGNORED" {
				t.Fatalf("dismissed source=%s", got)
			}
			var reopened string
			f.inTx(func(tx pgx.Tx) error {
				var err error
				reopened, err = ReopenReview(f.ctx, tx, ReopenCommand{HouseholdID: f.household, ActorUserID: f.user, ReviewItemID: item})
				return err
			})
			if reopened == "" || f.status(source) != "NEEDS_REVIEW" {
				t.Fatalf("reopened=%q source=%s", reopened, f.status(source))
			}
			// Settling the reopened item again goes through the same rule.
			f.inTx(func(tx pgx.Tx) error {
				return IgnoreReviewItem(f.ctx, tx, IgnoreCommand{HouseholdID: f.household, ActorUserID: f.user, ReviewItemID: reopened})
			})
			if got := f.status(source); got != "IGNORED" {
				t.Fatalf("re-dismissed source=%s", got)
			}
		})
	}
}
