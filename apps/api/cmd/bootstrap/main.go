package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/category"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/database"
)

func main() {
	var email, name, householdName string
	var telegramUserID int64
	flag.StringVar(&email, "email", "", "owner email address")
	flag.StringVar(&name, "name", "", "owner display name")
	flag.StringVar(&householdName, "household", "", "household name")
	flag.Int64Var(&telegramUserID, "telegram-user-id", 0, "authorized numeric Telegram user ID (optional)")
	flag.Parse()

	if err := run(context.Background(), email, name, householdName, telegramUserID, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap failed:", err)
		os.Exit(1)
	}
	fmt.Println("owner bootstrap completed")
}

func run(ctx context.Context, email, name, householdName string, telegramUserID int64, passwordInput *os.File) error {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	householdName = strings.TrimSpace(householdName)
	if email == "" || name == "" || householdName == "" {
		return fmt.Errorf("--email, --name, and --household are required")
	}
	if !strings.Contains(email, "@") {
		return fmt.Errorf("--email must be an email address")
	}
	if telegramUserID < 0 {
		return fmt.Errorf("--telegram-user-id must be a positive numeric ID")
	}
	password, err := bufio.NewReader(passwordInput).ReadString('\n')
	if err != nil && len(password) == 0 {
		return fmt.Errorf("read password from stdin: %w", err)
	}
	password = strings.TrimRight(password, "\r\n")
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	var ownerExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM household_member WHERE role = 'OWNER')`).Scan(&ownerExists); err != nil {
		return fmt.Errorf("check existing owner: %w", err)
	}
	if ownerExists {
		return fmt.Errorf("an owner already exists; bootstrap can run only once")
	}
	var householdID, userID string
	if err := tx.QueryRow(ctx, `INSERT INTO household (name) VALUES ($1) RETURNING id`, householdName).Scan(&householdID); err != nil {
		return fmt.Errorf("create household: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO "user" (email, display_name, password_hash) VALUES ($1, $2, $3) RETURNING id`, email, name, passwordHash).Scan(&userID); err != nil {
		return fmt.Errorf("create owner user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO household_member (household_id, user_id, role) VALUES ($1, $2, 'OWNER')`, householdID, userID); err != nil {
		return fmt.Errorf("create owner membership: %w", err)
	}
	if telegramUserID > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO telegram_identity (telegram_user_id,household_id,user_id) VALUES ($1,$2,$3)`, telegramUserID, householdID, userID); err != nil {
			return fmt.Errorf("authorize owner Telegram ID: %w", err)
		}
	}
	if err := category.SeedIndonesianDefaults(ctx, tx, householdID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO audit_log (household_id, actor_type, actor_id, action, entity_type, entity_id, after_json)
        VALUES ($1::uuid, 'SYSTEM', $2::uuid, 'BOOTSTRAP_OWNER', 'household', $1::uuid,
                jsonb_build_object('owner_user_id', $2::uuid, 'created_at', $3::timestamptz))`,
		householdID, userID, time.Now().UTC()); err != nil {
		return fmt.Errorf("audit bootstrap: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bootstrap: %w", err)
	}
	return nil
}
