package wealth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

func TestCorrectSnapshotPreservesHistoricalMembership(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stamp := time.Now().UnixNano()
	var household, user, oldAccount, currentAccount, laterAccount, snapshot string
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("wealth-correction-%d", stamp)).Scan(&household))
	must(pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Owner','unused') RETURNING id`, fmt.Sprintf("wealth-correction-%d@example.test", stamp)).Scan(&user))
	mustExec := func(sql string, args ...any) { _, err := pool.Exec(ctx, sql, args...); must(err) }
	mustExec(`INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user)
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Old Broker','ASSET','BROKERAGE','INVESTMENT') RETURNING id`, household).Scan(&oldAccount))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Current Cash','ASSET','CASH','TRANSACTIONAL') RETURNING id`, household).Scan(&currentAccount))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_snapshot(household_id,observed_at,created_by_user_id) VALUES($1,'2026-08-01T00:00:00+07:00',$2) RETURNING id`, household, user).Scan(&snapshot))
	mustExec(`INSERT INTO wealth_snapshot_item(snapshot_id,wealth_account_id,value_idr,source) VALUES($1,$2,100,'MANUAL'),($1,$3,200,'MANUAL')`, snapshot, oldAccount, currentAccount)
	var oldItemID, currentItemID string
	must(pool.QueryRow(ctx, `SELECT id::text FROM wealth_snapshot_item WHERE snapshot_id=$1 AND wealth_account_id=$2`, snapshot, oldAccount).Scan(&oldItemID))
	must(pool.QueryRow(ctx, `SELECT id::text FROM wealth_snapshot_item WHERE snapshot_id=$1 AND wealth_account_id=$2`, snapshot, currentAccount).Scan(&currentItemID))
	must(pool.QueryRow(ctx, `INSERT INTO wealth_account(household_id,name,side,wealth_type,usage_role) VALUES($1,'Gold Later','ASSET','GOLD','INVESTMENT') RETURNING id`, household).Scan(&laterAccount))
	mustExec(`UPDATE wealth_account SET active=false WHERE id=$1`, oldAccount)
	body, _ := json.Marshal(snapshotInput{ObservedAt: "2026-08-01T00:00:00+07:00", Items: []itemInput{{WealthAccountID: oldAccount, ValueIDR: "150", Source: "MANUAL"}, {WealthAccountID: currentAccount, ValueIDR: "200", Source: "MANUAL"}}})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/wealth/snapshots/"+snapshot, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", snapshot)
	r = r.WithContext(auth.ContextWithPrincipal(r.Context(), auth.Principal{UserID: user, HouseholdID: household, HouseholdRole: "OWNER", HasHousehold: true}))
	w := httptest.NewRecorder()
	NewHandler(pool).CorrectSnapshot(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count, oldCount, laterCount int
	must(pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE wealth_account_id=$2),count(*) FILTER(WHERE wealth_account_id=$3) FROM wealth_snapshot_item WHERE snapshot_id=$1`, snapshot, oldAccount, laterAccount).Scan(&count, &oldCount, &laterCount))
	if count != 2 || oldCount != 1 || laterCount != 0 {
		t.Fatalf("historical membership changed: count=%d old=%d later=%d", count, oldCount, laterCount)
	}
	var correctedOldID, correctedCurrentID, oldValue string
	must(pool.QueryRow(ctx, `SELECT id::text,value_idr::text FROM wealth_snapshot_item WHERE snapshot_id=$1 AND wealth_account_id=$2`, snapshot, oldAccount).Scan(&correctedOldID, &oldValue))
	must(pool.QueryRow(ctx, `SELECT id::text FROM wealth_snapshot_item WHERE snapshot_id=$1 AND wealth_account_id=$2`, snapshot, currentAccount).Scan(&correctedCurrentID))
	var audited bool
	must(pool.QueryRow(ctx, `SELECT before_json @> '{"items":[{"valueIdr":"100"}]}'::jsonb AND after_json @> '{"items":[{"valueIdr":"150"}]}'::jsonb FROM audit_log WHERE entity_id=$1 AND action='WEALTH_SNAPSHOT_UPDATE' ORDER BY created_at DESC LIMIT 1`, snapshot).Scan(&audited))
	if correctedOldID != oldItemID || correctedCurrentID != currentItemID || oldValue != "150" || !audited {
		t.Fatalf("ids=%s/%s %s/%s value=%s audited=%v", oldItemID, correctedOldID, currentItemID, correctedCurrentID, oldValue, audited)
	}
}
