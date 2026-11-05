package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// TestHouseholdListCountsEachFigureIndependently gives a household several
// rows in every child table, so a cartesian join would inflate the figures
// without DISTINCT and the list must still report exact per-table counts.
func TestHouseholdListCountsEachFigureIndependently(t *testing.T) {
	pool := provisionPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	name := fmt.Sprintf("List figures %d", stamp)
	var householdID, otherID string
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, name).Scan(&householdID))
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("List other %d", stamp)).Scan(&otherID))
	for i, active := range []bool{true, true, false} {
		var userID string
		must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','x') RETURNING id`, fmt.Sprintf("list-member-%d-%d@example.test", stamp, i)).Scan(&userID))
		_, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active) VALUES($1,$2,'MEMBER',$3)`, householdID, userID, active)
		must(err)
	}
	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',1000,now())`, householdID)
		must(err)
	}
	// Another household's rows must not leak into this one's figures.
	_, err := pool.Exec(ctx, `INSERT INTO transaction(household_id,type,status,amount,transaction_at) VALUES($1,'EXPENSE','NEEDS_REVIEW',1000,now())`, otherID)
	must(err)
	var latest time.Time
	for i := 0; i < 2; i++ {
		var sourceID string
		must(pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status,created_at) VALUES($1,'TELEGRAM_TEXT',$2,now(),$3,'NEEDS_REVIEW',now()-make_interval(hours => $4)) RETURNING id,created_at`, householdID, fmt.Sprintf("list-%d-%d", stamp, i), []byte(fmt.Sprint(stamp, i)), 2-i).Scan(&sourceID, &latest))
		for _, status := range []string{"OPEN", "RESOLVED"} {
			resolved := "NULL"
			if status == "RESOLVED" {
				resolved = "now()"
			}
			_, err := pool.Exec(ctx, `INSERT INTO review_item(household_id,source_event_id,review_type,status,resolved_at,decision) VALUES($1,$2,'AMBIGUOUS_CATEGORY',$3,`+resolved+`,'{"version":1,"reasonCode":"AMBIGUOUS_CATEGORY","allowedActions":["CONFIRM_REVIEW","IGNORE"]}'::jsonb)`, householdID, sourceID, status)
			must(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/households?q="+url.QueryEscape(name), nil)
	request = request.WithContext(auth.ContextWithPrincipal(ctx, auth.Principal{UserID: "00000000-0000-0000-0000-000000000000", IsSuperAdmin: true}))
	response := httptest.NewRecorder()
	h := NewHandler(pool, false, "responses")
	h.Require(http.HandlerFunc(h.Households)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	var list []struct {
		ID             string     `json:"id"`
		Members        int        `json:"members"`
		Transactions   int        `json:"transactions"`
		OpenReviews    int        `json:"openReviews"`
		LastActivityAt *time.Time `json:"lastActivityAt"`
	}
	must(json.Unmarshal(response.Body.Bytes(), &list))
	if len(list) != 1 || list[0].ID != householdID {
		t.Fatalf("search returned %+v, want only %s", list, householdID)
	}
	got := list[0]
	if got.Members != 2 || got.Transactions != 3 || got.OpenReviews != 2 || got.LastActivityAt == nil || !got.LastActivityAt.Equal(latest) {
		t.Fatalf("figures = %+v, want members=2 transactions=3 openReviews=2 lastActivityAt=%s", got, latest)
	}
}
