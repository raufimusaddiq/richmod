package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestLoginCannotOutliveAPasswordReset forces the interleaving where a login
// verified the old password and is about to create its session while a reset
// holds the user row: the reset must win, leaving no session behind.
func TestLoginCannotOutliveAPasswordReset(t *testing.T) {
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

	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("race-%d@example.test", stamp)
	oldHash, err := HashPassword("old-password-123456")
	if err != nil {
		t.Fatal(err)
	}
	var householdID, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Race %d", stamp)).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES($1,'Race',$2,now()) RETURNING id`, email, oldHash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'OWNER')`, householdID, userID); err != nil {
		t.Fatal(err)
	}

	// The reset has locked the user row but not committed yet.
	reset, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reset.Rollback(ctx)
	if _, err := reset.Exec(ctx, `SELECT 1 FROM "user" WHERE id=$1 FOR UPDATE`, userID); err != nil {
		t.Fatal(err)
	}

	loginErr := make(chan error, 1)
	go func() {
		_, _, _, err := NewService(pool).Login(ctx, email, "old-password-123456")
		loginErr <- err
	}()

	// Wait until the login has verified the old password and is blocked on the lock.
	blocked := false
	for i := 0; i < 100 && !blocked; i++ {
		_ = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'INSERT INTO session%')`).Scan(&blocked)
		time.Sleep(30 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("login never reached the session insert while the user row was locked")
	}

	newHash, err := HashPassword("brand-new-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reset.Exec(ctx, `UPDATE session SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := reset.Exec(ctx, `UPDATE "user" SET password_hash=$1 WHERE id=$2`, newHash, userID); err != nil {
		t.Fatal(err)
	}
	if err := reset.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-loginErr:
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("login after reset = %v, want ErrInvalidCredentials", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login did not finish after the reset committed")
	}
	var live int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM session WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&live); err != nil || live != 0 {
		t.Fatalf("live sessions after reset = %d err=%v", live, err)
	}
}
