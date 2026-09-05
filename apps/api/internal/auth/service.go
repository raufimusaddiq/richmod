package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrUnauthenticated = errors.New("unauthenticated")
var ErrTenantInvariantViolation = errors.New("user has multiple active household memberships")
var ErrHouseholdRequired = errors.New("household membership required")

const SessionIdleTimeout = 24 * time.Hour

type Membership struct {
	HouseholdID string `json:"id"`
	Role        string `json:"role"`
}

type Principal struct {
	UserID        string      `json:"userId"`
	Email         string      `json:"email"`
	DisplayName   string      `json:"displayName"`
	IsSuperAdmin  bool        `json:"isSuperAdmin"`
	Household     *Membership `json:"household"`
	HouseholdID   string      `json:"-"`
	HouseholdRole string      `json:"-"`
	HasHousehold  bool        `json:"-"`
	// Memberships is retained only for internal test/legacy construction. Runtime
	// tenant selection uses the canonical fields above.
	Memberships []Membership `json:"-"`
}

type TenantContext struct {
	UserID      string
	HouseholdID string
	Role        string
}

func TenantFromPrincipal(principal Principal) (TenantContext, error) {
	if !principal.HasHousehold || principal.HouseholdID == "" || principal.HouseholdRole == "" {
		return TenantContext{}, ErrHouseholdRequired
	}
	return TenantContext{UserID: principal.UserID, HouseholdID: principal.HouseholdID, Role: principal.HouseholdRole}, nil
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

// Login verifies a password and creates a session with a 24-hour idle expiry.
func (s *Service) Login(ctx context.Context, email, password string) (Principal, string, time.Time, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var userID, storedHash string
	var active, initialized bool
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash, active, password_initialized_at IS NOT NULL FROM "user" WHERE email = $1`, email).Scan(&userID, &storedHash, &active, &initialized)
	if err != nil || !active || !initialized {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	valid, err := VerifyPassword(storedHash, password)
	if err != nil || !valid {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	principal, err := s.principal(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrTenantInvariantViolation) {
			return Principal{UserID: userID}, "", time.Time{}, err
		}
		return Principal{}, "", time.Time{}, fmt.Errorf("load authenticated user: %w", err)
	}
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	expiresAt := s.now().Add(SessionIdleTimeout)
	if _, err := s.pool.Exec(ctx, `INSERT INTO session (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`, userID, tokenHash, expiresAt); err != nil {
		return Principal{}, "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return principal, token, expiresAt, nil
}

// Authenticate validates a session and renews its idle expiry for 24 hours.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, time.Time, error) {
	if token == "" {
		return Principal{}, time.Time{}, ErrUnauthenticated
	}
	tokenHash := hashToken(token)
	var userID string
	err := s.pool.QueryRow(ctx, `
        UPDATE session
        SET expires_at = $2
        WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $3
        RETURNING user_id`, tokenHash, s.now().Add(SessionIdleTimeout), s.now()).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, time.Time{}, ErrUnauthenticated
		}
		return Principal{}, time.Time{}, fmt.Errorf("renew session: %w", err)
	}
	principal, err := s.principal(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrTenantInvariantViolation) {
			return Principal{UserID: userID}, time.Time{}, err
		}
		return Principal{}, time.Time{}, fmt.Errorf("load authenticated user: %w", err)
	}
	return principal, s.now().Add(SessionIdleTimeout), nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE session SET revoked_at = $2 WHERE token_hash = $1 AND revoked_at IS NULL`, hashToken(token), s.now())
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) principal(ctx context.Context, userID string) (Principal, error) {
	principal := Principal{UserID: userID}
	if err := s.pool.QueryRow(ctx, `SELECT email, display_name, is_super_admin FROM "user" WHERE id = $1 AND active = TRUE`, userID).Scan(&principal.Email, &principal.DisplayName, &principal.IsSuperAdmin); err != nil {
		return Principal{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT household_id, role FROM household_member WHERE user_id = $1 AND active ORDER BY created_at LIMIT 2`, userID)
	if err != nil {
		return Principal{}, err
	}
	defer rows.Close()
	memberships := make([]Membership, 0, 2)
	for rows.Next() {
		var membership Membership
		if err := rows.Scan(&membership.HouseholdID, &membership.Role); err != nil {
			return Principal{}, err
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return Principal{}, err
	}
	return principalWithMemberships(principal, memberships)
}

func principalWithMemberships(principal Principal, memberships []Membership) (Principal, error) {
	switch len(memberships) {
	case 0:
		return principal, nil
	case 1:
		var membership Membership
		for _, item := range memberships {
			membership = item
		}
		principal.Household = &membership
		principal.HouseholdID = membership.HouseholdID
		principal.HouseholdRole = membership.Role
		principal.HasHousehold = true
		principal.Memberships = memberships
		return principal, nil
	default:
		return Principal{}, ErrTenantInvariantViolation
	}
}

func (s *Service) AcceptDashboardInvite(ctx context.Context, token, password string) (Principal, string, time.Time, error) {
	if len(password) < 12 || token == "" {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	h := hashToken(token)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	var inviteID, userID string
	err = tx.QueryRow(ctx, `SELECT id,user_id FROM dashboard_account_invite WHERE token_hash=$1 AND status='PENDING' AND expires_at>now() FOR UPDATE`, h).Scan(&inviteID, &userID)
	if err != nil {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE "user" SET password_hash=$1,password_initialized_at=now(),updated_at=now() WHERE id=$2 AND active`, hash, userID); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE dashboard_account_invite SET status='CONSUMED',consumed_at=now() WHERE id=$1`, inviteID); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	tokenOut, tokenHash, err := newSessionToken()
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	expires := s.now().Add(SessionIdleTimeout)
	if _, err = tx.Exec(ctx, `INSERT INTO session(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, userID, tokenHash, expires); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	p, err := s.principal(ctx, userID)
	return p, tokenOut, expires, err
}

func newSessionToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}
