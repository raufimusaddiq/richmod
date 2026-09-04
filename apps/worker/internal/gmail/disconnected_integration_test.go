package gmail

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDisconnectedHistoryJobIsTerminalNoop(t *testing.T) {
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
	var household, user, source string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES('gmail disconnected '||substr(gen_random_uuid()::text,1,8)) RETURNING id`).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES(('gmail-disconnected-'||substr(gen_random_uuid()::text,1,8)||'@test.invalid'),'Gmail Test','x',now()) RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, household, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gmail_integration(household_id,mailbox,encrypted_refresh_token,granted_scope,status,connected_by_user_id,history_id) VALUES($1,$2,$3,'readonly','DISCONNECTED',$4,'100')`, household, "mail-"+household+"@test.invalid", []byte("not-used"), user); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(household))
	if err := pool.QueryRow(ctx, `INSERT INTO source_event(household_id,source_type,external_id,received_at,payload_hash,processing_status) VALUES($1,'SYSTEM',$2,now(),$3,'RECEIVED') RETURNING id`, household, "late-gmail:"+household, digest[:]).Scan(&source); err != nil {
		t.Fatal(err)
	}
	processor := &Processor{pool: pool}
	if err := processor.ProcessHistory(ctx, HistoryPayload{SourceEventID: source, HistoryID: "101"}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT processing_status FROM source_event WHERE id=$1`, source).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "IGNORED" {
		t.Fatalf("late Gmail source status = %s", status)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_event WHERE household_id=$1 AND source_type='BANK_EMAIL'`, household).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("late Gmail job created %d bank events", count)
	}
}
