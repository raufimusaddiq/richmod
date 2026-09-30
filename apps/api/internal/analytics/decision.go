package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
)

var errDecisionInput = errors.New("decision text must be 1-2000 characters; cycleStart must select a closed salary cycle")
var errDecisionCycle = errors.New("decisions require a closed salary cycle")

type cycleDecision struct {
	ID         string    `json:"id"`
	CycleStart string    `json:"cycleStart"`
	Body       string    `json:"body"`
	Author     string    `json:"author"`
	AuthorID   string    `json:"authorUserId"`
	CreatedAt  time.Time `json:"createdAt"`
}

type decisionInput struct {
	CycleStart string `json:"cycleStart"`
	Body       string `json:"body"`
}

// Decisions includes the selected cycle and its immediate predecessor. Dates
// and the predecessor are resolved through the same server-owned review engine.
func (h *Handler) Decisions(w http.ResponseWriter, r *http.Request) {
	household, ok := analyticsHousehold(w, r)
	if !ok {
		return
	}
	start := r.URL.Query().Get("cycle_start")
	if _, err := time.Parse("2006-01-02", start); err != nil {
		writeJSON(w, 400, map[string]string{"error": "cycle_start must be YYYY-MM-DD"})
		return
	}
	items, err := h.listDecisions(r.Context(), household, start)
	if err != nil {
		decisionError(w, err)
		return
	}
	writeJSON(w, 200, items)
}

func (h *Handler) CreateDecision(w http.ResponseWriter, r *http.Request) {
	if _, ok := analyticsHousehold(w, r); !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	var in decisionInput
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if decodeDecision(r, &in) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid decision payload"})
		return
	}
	created, err := h.createDecision(r.Context(), p, in)
	if err != nil {
		decisionError(w, err)
		return
	}
	// createDecision has committed before this acknowledgement. Canonical text
	// and authorship remain only in cycle_decision, never generic telemetry.
	slog.InfoContext(r.Context(), "CYCLE_DECISION_SAVED")
	writeJSON(w, 201, created)
}

func (h *Handler) RevokeDecision(w http.ResponseWriter, r *http.Request) {
	if _, ok := analyticsHousehold(w, r); !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	id := r.PathValue("id")
	parsedID, err := uuid.Parse(id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid decision id"})
		return
	}
	if err := h.revokeDecision(r.Context(), p, parsedID.String()); err != nil {
		decisionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "REVOKED"})
}

func decisionError(w http.ResponseWriter, err error) {
	status, message := 500, "unable to access cycle decisions"
	switch {
	case errors.Is(err, errDecisionInput):
		status, message = 400, errDecisionInput.Error()
	case errors.Is(err, errDecisionCycle):
		status, message = 409, errDecisionCycle.Error()
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, analyticscore.ErrCycleNotFound):
		status, message = 404, "cycle or decision not found"
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeDecision(r *http.Request, value any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple values")
	}
	return nil
}

type decisionList struct {
	Items              []cycleDecision `json:"items"`
	Previous           []cycleDecision `json:"previous"`
	PreviousCycleStart *string         `json:"previousCycleStart"`
}

const decisionColumns = `d.id,d.cycle_start::text,d.body,u.display_name,d.created_by_user_id,d.created_at`

func scanDecision(scan func(...any) error) (cycleDecision, error) {
	var d cycleDecision
	err := scan(&d.ID, &d.CycleStart, &d.Body, &d.Author, &d.AuthorID, &d.CreatedAt)
	return d, err
}

func (h *Handler) listDecisions(ctx context.Context, household, start string) (decisionList, error) {
	result := decisionList{Items: []cycleDecision{}, Previous: []cycleDecision{}}
	facts, err := analyticscore.Load(ctx, h.pool, household, start, cycleNow(h)())
	if err != nil {
		return result, err
	}
	if facts.Comparison.Previous != nil {
		result.PreviousCycleStart = &facts.Comparison.Previous.Start
	}
	rows, err := h.pool.Query(ctx, `SELECT `+decisionColumns+` FROM cycle_decision d JOIN "user" u ON u.id=d.created_by_user_id WHERE d.household_id=$1 AND d.deleted_at IS NULL AND (d.cycle_start=$2::date OR d.cycle_start=$3::date) ORDER BY d.created_at,d.id`, household, start, result.PreviousCycleStart)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDecision(rows.Scan)
		if err != nil {
			return result, err
		}
		if d.CycleStart == start {
			result.Items = append(result.Items, d)
		} else {
			result.Previous = append(result.Previous, d)
		}
	}
	return result, rows.Err()
}

// createDecision writes only the human-authored note and audit record in one
// transaction. Financial facts are read, never mutated.
func (h *Handler) createDecision(ctx context.Context, p auth.Principal, in decisionInput) (cycleDecision, error) {
	in.Body = strings.TrimSpace(in.Body)
	if _, err := time.Parse("2006-01-02", in.CycleStart); err != nil {
		return cycleDecision{}, errDecisionInput
	}
	if in.Body == "" || !utf8.ValidString(in.Body) || strings.ContainsRune(in.Body, 0) || utf8.RuneCountInString(in.Body) > 2000 {
		return cycleDecision{}, errDecisionInput
	}
	facts, err := analyticscore.Load(ctx, h.pool, p.HouseholdID, in.CycleStart, cycleNow(h)())
	if err != nil {
		return cycleDecision{}, err
	}
	if facts.Period.Kind != "SALARY_CYCLE" || facts.Period.State != "CLOSED" {
		return cycleDecision{}, errDecisionCycle
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return cycleDecision{}, err
	}
	defer tx.Rollback(ctx)
	var id string
	// Recheck membership at the write boundary; no client-supplied household or author.
	if err := tx.QueryRow(ctx, `INSERT INTO cycle_decision(household_id,cycle_start,body,created_by_user_id) SELECT $1,$2,$3,$4 WHERE EXISTS(SELECT 1 FROM household_member WHERE household_id=$1 AND user_id=$4 AND active) RETURNING id`, p.HouseholdID, in.CycleStart, in.Body, p.UserID).Scan(&id); err != nil {
		return cycleDecision{}, err
	}
	if err := auditDecision(ctx, tx, p, id, "CYCLE_DECISION_CREATE"); err != nil {
		return cycleDecision{}, err
	}
	created, err := scanDecision(tx.QueryRow(ctx, `SELECT `+decisionColumns+` FROM cycle_decision d JOIN "user" u ON u.id=d.created_by_user_id WHERE d.id=$1 AND d.household_id=$2`, id, p.HouseholdID).Scan)
	if err != nil {
		return cycleDecision{}, err
	}
	return created, tx.Commit(ctx)
}

func (h *Handler) revokeDecision(ctx context.Context, p auth.Principal, id string) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Shared household decisions may be revoked by any active household member.
	result, err := tx.Exec(ctx, `UPDATE cycle_decision SET deleted_at=now() WHERE id=$1 AND household_id=$2 AND deleted_at IS NULL AND EXISTS(SELECT 1 FROM household_member WHERE household_id=$2 AND user_id=$3 AND active)`, id, p.HouseholdID, p.UserID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err := auditDecision(ctx, tx, p, id, "CYCLE_DECISION_REVOKE"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func auditDecision(ctx context.Context, tx pgx.Tx, p auth.Principal, id, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id) VALUES($1,'USER',$2,$3,'cycle_decision',$4)`, p.HouseholdID, p.UserID, action, id)
	return err
}
