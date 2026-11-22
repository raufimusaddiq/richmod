package provision

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createInTx(t *testing.T, pool *pgxpool.Pool, in HouseholdInput) (Household, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	created, err := CreateHousehold(ctx, tx, in)
	if err != nil {
		return Household{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return created, nil
}

func baseInput(stamp int64, email string) HouseholdInput {
	return HouseholdInput{
		HouseholdName: fmt.Sprintf("Provision %d", stamp),
		OwnerName:     "Pemilik",
		OwnerEmail:    email,
		PasswordHash:  "unusable-new-hash",
		ActorType:     "USER",
		Action:        "CREATE_HOUSEHOLD",
	}
}

func TestCreateHouseholdAdoptsGoogleUserWithoutHousehold(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("google-owner-%d@example.test", stamp)
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,google_sub,google_linked_at) VALUES($1,'Google Name','google-only-hash',$2,now()) RETURNING id::text`, email, fmt.Sprintf("sub-%d", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	created, err := createInTx(t, pool, baseInput(stamp, email))
	if err != nil {
		t.Fatalf("create household for google user: %v", err)
	}
	if created.OwnerUserID != userID {
		t.Fatalf("owner user = %s, want the existing google user %s", created.OwnerUserID, userID)
	}
	var displayName, hash, role string
	var passwordInitialized, googleLinked, memberActive bool
	if err := pool.QueryRow(ctx, `SELECT u.display_name,u.password_hash,u.password_initialized_at IS NOT NULL,u.google_sub IS NOT NULL,hm.role,hm.active
		FROM "user" u JOIN household_member hm ON hm.user_id=u.id AND hm.household_id=$2 WHERE u.id=$1`, userID, created.ID).Scan(&displayName, &hash, &passwordInitialized, &googleLinked, &role, &memberActive); err != nil {
		t.Fatal(err)
	}
	if displayName != "Pemilik" || hash != "google-only-hash" || passwordInitialized || !googleLinked || role != "OWNER" || !memberActive {
		t.Fatalf("displayName=%q hash=%q passwordInitialized=%v googleLinked=%v role=%s active=%v", displayName, hash, passwordInitialized, googleLinked, role, memberActive)
	}
	var users int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM "user" WHERE email=$1`, email).Scan(&users); err != nil || users != 1 {
		t.Fatalf("users=%d err=%v", users, err)
	}
}

func TestCreateHouseholdAdoptsUnenrolledExistingUserWithNewPlaceholderHash(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("bare-owner-%d@example.test", stamp)
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Bare','old-hash') RETURNING id::text`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	created, err := createInTx(t, pool, baseInput(stamp, email))
	if err != nil || created.OwnerUserID != userID {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	var hash string
	var initialized bool
	if err := pool.QueryRow(ctx, `SELECT password_hash,password_initialized_at IS NOT NULL FROM "user" WHERE id=$1`, userID).Scan(&hash, &initialized); err != nil {
		t.Fatal(err)
	}
	if hash != "unusable-new-hash" || initialized {
		t.Fatalf("hash=%q initialized=%v", hash, initialized)
	}
}

func TestCreateHouseholdWithPasswordOverwritesExistingCredentials(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("password-owner-%d@example.test", stamp)
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,google_sub,google_linked_at) VALUES($1,'G','old-hash',$2,now()) RETURNING id::text`, email, fmt.Sprintf("sub-pw-%d", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	in := baseInput(stamp, email)
	in.PasswordHash = "chosen-hash"
	in.PasswordSet = true
	created, err := createInTx(t, pool, in)
	if err != nil || created.OwnerUserID != userID {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	var hash string
	var initialized bool
	if err := pool.QueryRow(ctx, `SELECT password_hash,password_initialized_at IS NOT NULL FROM "user" WHERE id=$1`, userID).Scan(&hash, &initialized); err != nil {
		t.Fatal(err)
	}
	if hash != "chosen-hash" || !initialized {
		t.Fatalf("hash=%q initialized=%v", hash, initialized)
	}
}

func TestCreateHouseholdReassignsDeactivatedMemberAndRebindsTelegram(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("reassigned-%d@example.test", stamp)
	telegramID := stamp%6_000_000_000 + 101
	var oldHousehold, userID string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id::text`, fmt.Sprintf("Old %d", stamp)).Scan(&oldHousehold); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash,password_initialized_at) VALUES($1,'Former','kept-hash',now()) RETURNING id::text`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role,active,deactivated_at) VALUES($1,$2,'MEMBER',false,now())`, oldHousehold, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identity(telegram_user_id,household_id,user_id,active) VALUES($1,$2,$3,false)`, telegramID, oldHousehold, userID); err != nil {
		t.Fatal(err)
	}

	in := baseInput(stamp, email)
	in.TelegramUserID = telegramID
	created, err := createInTx(t, pool, in)
	if err != nil {
		t.Fatalf("reassign deactivated member: %v", err)
	}
	if created.OwnerUserID != userID || created.ID == oldHousehold {
		t.Fatalf("created=%+v userID=%s oldHousehold=%s", created, userID, oldHousehold)
	}
	var identityHousehold string
	var identityActive bool
	if err := pool.QueryRow(ctx, `SELECT household_id::text,active FROM telegram_identity WHERE telegram_user_id=$1`, telegramID).Scan(&identityHousehold, &identityActive); err != nil {
		t.Fatal(err)
	}
	if identityHousehold != created.ID || !identityActive {
		t.Fatalf("telegram identity household=%s active=%v, want %s true", identityHousehold, identityActive, created.ID)
	}
	var oldActive, initialized bool
	var activeMemberships int
	var hash string
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT active FROM household_member WHERE household_id=$2 AND user_id=$1),
		(SELECT count(*) FROM household_member WHERE user_id=$1 AND active),
		u.password_hash,u.password_initialized_at IS NOT NULL
		FROM "user" u WHERE u.id=$1`, userID, oldHousehold).Scan(&oldActive, &activeMemberships, &hash, &initialized); err != nil {
		t.Fatal(err)
	}
	if oldActive || activeMemberships != 1 || hash != "kept-hash" || !initialized {
		t.Fatalf("oldActive=%v activeMemberships=%d hash=%q initialized=%v", oldActive, activeMemberships, hash, initialized)
	}
}

func TestCreateHouseholdRefusesUserWithActiveHouseholdOrInactiveAccount(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	activeEmail := fmt.Sprintf("member-%d@example.test", stamp)
	inactiveEmail := fmt.Sprintf("disabled-%d@example.test", stamp)
	var household, activeUser string
	if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id::text`, fmt.Sprintf("Occupied %d", stamp)).Scan(&household); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user"(email,display_name,password_hash) VALUES($1,'Member','x') RETURNING id::text`, activeEmail).Scan(&activeUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO household_member(household_id,user_id,role) VALUES($1,$2,'MEMBER')`, household, activeUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO "user"(email,display_name,password_hash,active) VALUES($1,'Disabled','x',false)`, inactiveEmail); err != nil {
		t.Fatal(err)
	}
	for name, email := range map[string]string{"active household": activeEmail, "inactive account": inactiveEmail} {
		if _, err := createInTx(t, pool, baseInput(stamp, email)); !errors.Is(err, ErrUserExists) {
			t.Errorf("%s: CreateHousehold() = %v, want ErrUserExists", name, err)
		}
	}
	var households int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM household WHERE name=$1`, fmt.Sprintf("Provision %d", stamp)).Scan(&households); err != nil || households != 0 {
		t.Fatalf("households created=%d err=%v", households, err)
	}
}
