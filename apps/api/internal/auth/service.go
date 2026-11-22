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
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrUnauthenticated = errors.New("unauthenticated")
var ErrTenantInvariantViolation = errors.New("user has multiple active household memberships")
var ErrInviteRequired = errors.New("invite acceptance required")
var ErrGoogleEmailMismatch = errors.New("google email does not match invite")

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
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

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
	created, err := s.pool.Exec(ctx, `INSERT INTO session (user_id, token_hash, expires_at)
		SELECT id, $2, $3 FROM "user" WHERE id=$1 AND active AND password_hash=$4 AND password_initialized_at IS NOT NULL FOR SHARE`, userID, tokenHash, expiresAt, storedHash)
	if err != nil {
		return Principal{}, "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	if created.RowsAffected() == 0 {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	return principal, token, expiresAt, nil
}

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
		membership := memberships[0]
		principal.Household = &membership
		principal.HouseholdID = membership.HouseholdID
		principal.HouseholdRole = membership.Role
		principal.HasHousehold = true
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
	err = tx.QueryRow(ctx, `SELECT i.id,i.user_id FROM dashboard_account_invite i
		JOIN household_member hm ON hm.household_id=i.household_id AND hm.user_id=i.user_id AND hm.active
		WHERE i.token_hash=$1 AND i.status='PENDING' AND i.expires_at>now() FOR UPDATE OF i`, h).Scan(&inviteID, &userID)
	if err != nil {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	var hadPassword bool
	if err = tx.QueryRow(ctx, `SELECT password_initialized_at IS NOT NULL FROM "user" WHERE id=$1 FOR UPDATE`, userID).Scan(&hadPassword); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	if hadPassword {
		if _, err = tx.Exec(ctx, `UPDATE session SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
			return Principal{}, "", time.Time{}, err
		}
	}
	updated, err := tx.Exec(ctx, `UPDATE "user" SET password_hash=$1,password_initialized_at=now(),updated_at=now() WHERE id=$2 AND active`, hash, userID)
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	if updated.RowsAffected() != 1 {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
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

func normalizeGoogleClaims(claims GoogleClaims) (email, sub string) {
	return strings.ToLower(strings.TrimSpace(claims.Email)), strings.TrimSpace(claims.Subject)
}

func googleDisplayName(claims GoogleClaims, email string) string {
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	if name == "" {
		name = email
	}
	if utf8.RuneCountInString(name) > 120 {
		name = strings.TrimSpace(string([]rune(name)[:120]))
	}
	return name
}

func (s *Service) LoginWithGoogle(ctx context.Context, claims GoogleClaims) (Principal, string, time.Time, error) {
	email, sub := normalizeGoogleClaims(claims)
	if email == "" || sub == "" {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('richmod.household.create'))`); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	var userID string
	var active bool
	err = tx.QueryRow(ctx, `SELECT id, active FROM "user" WHERE google_sub = $1 FOR UPDATE`, sub).Scan(&userID, &active)
	switch {
	case err == nil:
		if !active {
			return Principal{}, "", time.Time{}, ErrInvalidCredentials
		}
	case errors.Is(err, pgx.ErrNoRows):
		userID, err = s.linkOrCreateGoogleUser(ctx, tx, claims, email, sub)
		if err != nil {
			return Principal{}, "", time.Time{}, err
		}
	default:
		return Principal{}, "", time.Time{}, fmt.Errorf("find google user: %w", err)
	}
	return s.finishGoogleSession(ctx, tx, userID)
}

func (s *Service) linkOrCreateGoogleUser(ctx context.Context, tx pgx.Tx, claims GoogleClaims, email, sub string) (string, error) {
	var userID string
	var active, passwordInitialized bool
	var existingSub *string
	err := tx.QueryRow(ctx, `SELECT id, active, password_initialized_at IS NOT NULL, google_sub FROM "user" WHERE email = $1 FOR UPDATE`, email).Scan(&userID, &active, &passwordInitialized, &existingSub)
	switch {
	case err == nil:
		if !active {
			return "", ErrInvalidCredentials
		}
		if existingSub != nil && *existingSub != sub {
			return "", ErrInvalidCredentials
		}
		if !passwordInitialized && existingSub == nil {
			return "", ErrInviteRequired
		}
		updated, err := tx.Exec(ctx, `UPDATE "user" SET google_sub = $1, google_linked_at = now(), updated_at = now() WHERE id = $2 AND active`, sub, userID)
		if err != nil {
			return "", fmt.Errorf("link google account: %w", err)
		}
		if updated.RowsAffected() != 1 {
			return "", ErrInvalidCredentials
		}
		return userID, nil
	case errors.Is(err, pgx.ErrNoRows):
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate placeholder password: %w", err)
		}
		hash, err := HashPassword(base64.RawURLEncoding.EncodeToString(raw))
		if err != nil {
			return "", err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO "user" (email, display_name, password_hash, password_initialized_at, google_sub, google_linked_at, is_super_admin)
			VALUES ($1, $2, $3, NULL, $4, now(), FALSE) RETURNING id`, email, googleDisplayName(claims, email), hash, sub).Scan(&userID); err != nil {
			return "", fmt.Errorf("create google user: %w", err)
		}
		return userID, nil
	default:
		return "", fmt.Errorf("find user by email: %w", err)
	}
}

func (s *Service) finishGoogleSession(ctx context.Context, tx pgx.Tx, userID string) (Principal, string, time.Time, error) {
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	expiresAt := s.now().Add(SessionIdleTimeout)
	if _, err = tx.Exec(ctx, `INSERT INTO session (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`, userID, tokenHash, expiresAt); err != nil {
		return Principal{}, "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Principal{}, "", time.Time{}, err
	}
	principal, err := s.principal(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrTenantInvariantViolation) {
			return Principal{UserID: userID}, "", time.Time{}, err
		}
		return Principal{}, "", time.Time{}, fmt.Errorf("load authenticated user: %w", err)
	}
	return principal, token, expiresAt, nil
}

func (s *Service) AcceptDashboardInviteWithGoogle(ctx context.Context, inviteToken string, claims GoogleClaims) (Principal, string, time.Time, error) {
	email, sub := normalizeGoogleClaims(claims)
	if inviteToken == "" || email == "" || sub == "" {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Principal{}, "", time.Time{}, err
	}
	defer tx.Rollback(ctx)
	var inviteID, userID, invitedEmail string
	var existingSub *string
	var hadPassword bool
	err = tx.QueryRow(ctx, `SELECT i.id, i.user_id, u.email, u.google_sub, u.password_initialized_at IS NOT NULL
		FROM dashboard_account_invite i
		JOIN household_member hm ON hm.household_id = i.household_id AND hm.user_id = i.user_id AND hm.active
		JOIN "user" u ON u.id = i.user_id AND u.active
		WHERE i.token_hash = $1 AND i.status = 'PENDING' AND i.expires_at > now()
		FOR UPDATE OF i, u`, hashToken(inviteToken)).Scan(&inviteID, &userID, &invitedEmail, &existingSub, &hadPassword)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, "", time.Time{}, ErrInvalidCredentials
		}
		return Principal{}, "", time.Time{}, fmt.Errorf("load invite: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(invitedEmail)) != email {
		return Principal{}, "", time.Time{}, ErrGoogleEmailMismatch
	}
	if existingSub != nil && *existingSub != sub {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	var taken bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM "user" WHERE google_sub = $1 AND id <> $2)`, sub, userID).Scan(&taken); err != nil {
		return Principal{}, "", time.Time{}, fmt.Errorf("check google subject: %w", err)
	}
	if taken {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	if hadPassword || existingSub != nil {
		if _, err = tx.Exec(ctx, `UPDATE session SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
			return Principal{}, "", time.Time{}, fmt.Errorf("revoke sessions: %w", err)
		}
	}
	updated, err := tx.Exec(ctx, `UPDATE "user" SET google_sub = $1, google_linked_at = COALESCE(google_linked_at, now()), updated_at = now() WHERE id = $2 AND active`, sub, userID)
	if err != nil {
		return Principal{}, "", time.Time{}, fmt.Errorf("link google account: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return Principal{}, "", time.Time{}, ErrInvalidCredentials
	}
	if _, err = tx.Exec(ctx, `UPDATE dashboard_account_invite SET status = 'CONSUMED', consumed_at = now() WHERE id = $1`, inviteID); err != nil {
		return Principal{}, "", time.Time{}, fmt.Errorf("consume invite: %w", err)
	}
	return s.finishGoogleSession(ctx, tx, userID)
}
