package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/api/internal/platform/database"
)

func main() {
	var email, name, householdName string
	var promote, newHousehold bool
	var telegramUserID int64
	flag.StringVar(&email, "email", "", "owner email address")
	flag.StringVar(&name, "name", "", "owner display name")
	flag.StringVar(&householdName, "household", "", "household name")
	flag.Int64Var(&telegramUserID, "telegram-user-id", 0, "authorized numeric Telegram user ID (optional)")
	flag.BoolVar(&promote, "promote-super-admin", false, "grant global super-admin to an existing user")
	flag.BoolVar(&newHousehold, "new-household", false, "create an additional household with a new owner (after the first bootstrap)")
	flag.Parse()

	if promote { if err:=promoteSuperAdmin(context.Background(),email); err!=nil { fmt.Fprintln(os.Stderr,"promotion failed:",err); os.Exit(1) }; fmt.Println("super-admin promotion completed"); return }
	mode := modeFirstOwner
	if newHousehold {
		mode = modeAdditionalHousehold
	}
	if err := run(context.Background(), mode, email, name, householdName, telegramUserID, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap failed:", err)
		os.Exit(1)
	}
	if newHousehold {
		fmt.Println("household created")
		return
	}
	fmt.Println("owner bootstrap completed")
}

func run(ctx context.Context, mode bootstrapMode, email, name, householdName string, telegramUserID int64, passwordInput *os.File) error {
	in, err := normalizeInput(email, name, householdName, telegramUserID)
	if err != nil {
		return err
	}
	password, err := bufio.NewReader(passwordInput).ReadString('\n')
	if err != nil && len(password) == 0 {
		return fmt.Errorf("read password from stdin: %w", err)
	}
	in.PasswordHash, err = auth.HashPassword(strings.TrimRight(password, "\r\n"))
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
	_, err = createHousehold(ctx, pool, mode, in)
	return err
}

func promoteSuperAdmin(ctx context.Context, email string) error {
	email=strings.ToLower(strings.TrimSpace(email)); if !strings.Contains(email,"@"){return fmt.Errorf("--email must be an email address")}; databaseURL:=os.Getenv("DATABASE_URL");if databaseURL==""{return fmt.Errorf("DATABASE_URL is required")};pool,err:=database.Open(ctx,databaseURL);if err!=nil{return err};defer pool.Close();var id string;if err=pool.QueryRow(ctx,`UPDATE "user" SET is_super_admin=TRUE,updated_at=now() WHERE email=$1 RETURNING id`,email).Scan(&id);err!=nil{return fmt.Errorf("user not found: %w",err)};return nil
}
