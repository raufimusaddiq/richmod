package reviewdomain

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Each lane's resolution leaves its source in the status the shared lifecycle
// derives: an accepted sibling wins, an open sibling keeps it in review, and a
// lone dismissal is IGNORED.
func TestFinancialEmailIgnoreSettlesThroughTheSharedLifecycle(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, tt := range []struct{ sibling, want string }{{"", "IGNORED"}, {"APPLIED", "PROCESSED"}, {"REVIEW", "NEEDS_REVIEW"}} {
		t.Run("sibling "+tt.sibling, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			stamp := time.Now().UnixNano()
			var household, user, source, observation, item string
			must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Lane lifecycle %d", stamp)).Scan(&household))
			must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("lane-%d@test.invalid", stamp)).Scan(&user))
			must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'FINANCIAL_EMAIL',$2,now(),$3,'NEEDS_REVIEW') RETURNING id`, household, fmt.Sprint(stamp), []byte(fmt.Sprint(stamp))).Scan(&source))
			must(pool.QueryRow(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,0,'CASH_MOVEMENT','{}'::jsonb,'REVIEW') RETURNING id`, household, source).Scan(&observation))
			if tt.sibling != "" {
				_, err := pool.Exec(ctx, `INSERT INTO financial_email_observation(household_id,source_event_id,ordinal,kind,facts_json,status) VALUES($1,$2,1,'CASH_MOVEMENT','{}'::jsonb,$3)`, household, source, tt.sibling)
				must(err)
			}
			must(pool.QueryRow(ctx, `INSERT INTO review_item(household_id,financial_email_observation_id,review_type,status,decision) VALUES($1,$2,'FINANCIAL_EMAIL_RESOLUTION','OPEN','{"version":1,"reasonCode":"FINANCIAL_EMAIL_RESOLUTION","allowedActions":["SET_FINANCIAL_EMAIL_ENTITIES","IGNORE"]}'::jsonb) RETURNING id`, household, observation).Scan(&item))
			tx, err := pool.Begin(ctx)
			must(err)
			defer tx.Rollback(ctx)
			_, err = ResolveFinancialEmailReview(ctx, tx, FinancialEmailCommand{HouseholdID: household, ObservationID: observation, ReviewItemID: item, ActorUserID: user, Ignore: true})
			must(err)
			must(tx.Commit(ctx))
			var status string
			must(pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status))
			if status != tt.want {
				t.Fatalf("source=%s want %s", status, tt.want)
			}
		})
	}
}
