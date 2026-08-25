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

const SessionIdleTimeout = 24 * time.Hour

type Membership struct {
	HouseholdID string `json:"householdId"`
	Role        string `json:"role"`
}

type Principal struct {
	UserID      string       `json:"userId"`
	Email       string       `json:"email"`
	DisplayName string       `json:"displayName"`
	Memberships []Membership `json:"memberships"`
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
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash, active FROM "user" WHERE email = $1`, email).Scan(&userID, &storedHash, &active)
	if err != nil || !active {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	valid, err := VerifyPassword(storedHash, password)
	if err != nil || !valid {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	principal, err := s.principal(ctx, userID)
	if err != nil {
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
	if err := s.pool.QueryRow(ctx, `SELECT email, display_name FROM "user" WHERE id = $1 AND active = TRUE`, userID).Scan(&principal.Email, &principal.DisplayName); err != nil {
		return Principal{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT household_id, role FROM household_member WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return Principal{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var membership Membership
		if err := rows.Scan(&membership.HouseholdID, &membership.Role); err != nil {
			return Principal{}, err
		}
		principal.Memberships = append(principal.Memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return Principal{}, err
	}
	return principal, nil
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
