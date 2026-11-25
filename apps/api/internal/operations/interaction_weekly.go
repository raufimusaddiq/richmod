package operations

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

const (
	interactionWindowDays   = 7
	interactionBudget       = time.Second
	interactionProbeTimeout = 800 * time.Millisecond
	mostlyTelegramNumerator = 7
	mostlyTelegramDenom     = 10
	householdZone           = "Asia/Jakarta"
)

var answerSourceKeys = []string{"TAP", "REPLY", "THREAD_REPLY", "PLAIN", "LABEL_TEXT", "VIA_ASSISTANT", "UNKNOWN"}

type interactionTypeRow struct {
	ReviewType       string `json:"reviewType"`
	Sent             int    `json:"sent"`
	FinishedTelegram int    `json:"finishedTelegram"`
	FinishedWeb      int    `json:"finishedWeb"`
	StillOpen        int    `json:"stillOpen"`
}

type interactionWindow struct {
	Start               time.Time            `json:"start"`
	End                 time.Time            `json:"end"`
	CardsSent           int                  `json:"cardsSent"`
	FinishedTelegram    int                  `json:"finishedTelegram"`
	FinishedWeb         int                  `json:"finishedWeb"`
	StillOpen           int                  `json:"stillOpen"`
	FinishedRate        *float64             `json:"finishedTelegramRate"`
	TypesSent           int                  `json:"typesSent"`
	TypesMostlyTelegram int                  `json:"typesMostlyTelegram"`
	Types               []interactionTypeRow `json:"types"`
	Answers             int                  `json:"answers"`
	UnusableAnswers     int                  `json:"unusableAnswers"`
	UnusableRate        *float64             `json:"unusableAnswerRate"`
	AnswerSources       map[string]int       `json:"answerSources"`
	RepeatedInput       int                  `json:"repeatedInput"`
	BrokenReplies       int                  `json:"brokenReplies"`
	MechanicsReplies    int                  `json:"mechanicsReplies"`
	MedianFinishMs      *int64               `json:"medianFinishMs"`
}

type interactionWeekly struct {
	WindowDays       int               `json:"windowDays"`
	Timezone         string            `json:"timezone"`
	DegradedSections []string          `json:"degradedSections"`
	Current          interactionWindow `json:"current"`
	Previous         interactionWindow `json:"previous"`
	NotYetMeasurable []string          `json:"notYetMeasurable"`
}

func interactionWindows(now time.Time) (previous, current interactionWindow, err error) {
	loc, err := time.LoadLocation(householdZone)
	if err != nil {
		return previous, current, err
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	current = newInteractionWindow(today.AddDate(0, 0, 1-interactionWindowDays), today.AddDate(0, 0, 1))
	previous = newInteractionWindow(today.AddDate(0, 0, 1-2*interactionWindowDays), current.Start)
	return previous, current, nil
}

func newInteractionWindow(start, end time.Time) interactionWindow {
	window := interactionWindow{Start: start, End: end, Types: []interactionTypeRow{}, AnswerSources: map[string]int{}}
	for _, key := range answerSourceKeys {
		window.AnswerSources[key] = 0
	}
	return window
}

func rateOf(count, of int) *float64 {
	if of <= 0 {
		return nil
	}
	rate := float64(count) / float64(of)
	return &rate
}

func (w *interactionWindow) finalize() {
	sort.Slice(w.Types, func(i, j int) bool { return w.Types[i].ReviewType < w.Types[j].ReviewType })
	for _, row := range w.Types {
		w.CardsSent += row.Sent
		w.FinishedTelegram += row.FinishedTelegram
		w.FinishedWeb += row.FinishedWeb
		w.StillOpen += row.StillOpen
		if row.Sent > 0 {
			w.TypesSent++
			if row.FinishedTelegram*mostlyTelegramDenom > row.Sent*mostlyTelegramNumerator {
				w.TypesMostlyTelegram++
			}
		}
	}
	w.FinishedRate = rateOf(w.FinishedTelegram, w.CardsSent)
	w.UnusableRate = rateOf(w.UnusableAnswers, w.Answers+w.UnusableAnswers)
}

const interactionCardsCTE = `
	WITH cards AS (
	  SELECT ri.id, ri.review_type, ri.status, ri.resolved_at, s.sent_at,
	    CASE WHEN s.sent_at >= $2 THEN 'current' ELSE 'previous' END AS win,
	    EXISTS (SELECT 1 FROM product_telemetry_event e WHERE e.household_id=ri.household_id AND e.review_item_id=ri.id
	      AND e.event_type='INTERACTION' AND e.action='FINISHED_ON_WEB') AS on_web,
	    EXISTS (SELECT 1 FROM product_telemetry_event e WHERE e.household_id=ri.household_id AND e.review_item_id=ri.id
	      AND e.event_type='INTERACTION' AND e.action='ANSWERED') AS answered
	  FROM review_item ri
	  CROSS JOIN LATERAL (
	    SELECT min(m.created_at) AS sent_at
	    FROM review_request r JOIN review_request_message m ON m.review_request_id=r.id
	    WHERE r.review_item_id=ri.id AND r.household_id=ri.household_id
	  ) s
	  WHERE ri.household_id=$1 AND s.sent_at >= $3 AND s.sent_at < $4
	), outcomes AS (
	  SELECT *,
	    (status='RESOLVED' AND on_web) AS finished_web,
	    (status='RESOLVED' AND NOT on_web AND answered) AS finished_telegram,
	    (status IN ('OPEN','PENDING_SEND')) AS still_open
	  FROM cards
	)`

func (h *Handler) loadInteractionWeekly(ctx context.Context, householdID string, now time.Time) (interactionWeekly, error) {
	previous, current, err := interactionWindows(now)
	if err != nil {
		return interactionWeekly{}, err
	}
	summary := interactionWeekly{WindowDays: interactionWindowDays, Timezone: householdZone, NotYetMeasurable: []string{}}
	windows := map[string]*interactionWindow{"current": &current, "previous": &previous}

	timeout := h.probeTimeout
	if timeout == 0 {
		timeout = interactionProbeTimeout
	}
	section := func(name string, fn func(context.Context) error) namedGroup {
		return namedGroup{name, func(ctx context.Context) error { return h.probeWithin(ctx, timeout, fn) }}
	}
	degraded := runGroups(ctx,
		section("cards", func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, interactionCardsCTE+`
				SELECT win, review_type, count(*), count(*) FILTER (WHERE finished_telegram),
				  count(*) FILTER (WHERE finished_web), count(*) FILTER (WHERE still_open)
				FROM outcomes GROUP BY win, review_type`, householdID, current.Start, previous.Start, current.End)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var win string
				var row interactionTypeRow
				if err := rows.Scan(&win, &row.ReviewType, &row.Sent, &row.FinishedTelegram, &row.FinishedWeb, &row.StillOpen); err != nil {
					return err
				}
				windows[win].Types = append(windows[win].Types, row)
			}
			return rows.Err()
		}),
		section("median", func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, interactionCardsCTE+`
				SELECT win, (percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM (resolved_at - sent_at))*1000))::bigint
				FROM outcomes WHERE finished_telegram AND resolved_at >= sent_at GROUP BY win`, householdID, current.Start, previous.Start, current.End)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var win string
				var median int64
				if err := rows.Scan(&win, &median); err != nil {
					return err
				}
				windows[win].MedianFinishMs = &median
			}
			return rows.Err()
		}),
		section("events", func(ctx context.Context) error {
			rows, err := h.pool.Query(ctx, `
				SELECT CASE WHEN occurred_at >= $2 THEN 'current' ELSE 'previous' END,
				  action, COALESCE(source_type,''), COALESCE(decision_source,''), count(*)
				FROM product_telemetry_event
				WHERE household_id=$1 AND event_type='INTERACTION' AND occurred_at >= $3 AND occurred_at < $4
				  AND action IN ('ANSWERED','UNMATCHED','AMBIGUOUS','CLOSED_CARD_REPLY','REPEATED_INPUT','REPLY_REPAIRED')
				GROUP BY 1,2,3,4`, householdID, current.Start, previous.Start, current.End)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var win, action, source, decision string
				var count int
				if err := rows.Scan(&win, &action, &source, &decision, &count); err != nil {
					return err
				}
				window := windows[win]
				switch action {
				case "ANSWERED":
					window.Answers += count
					if source == "" {
						source = "UNKNOWN"
					}
					if _, known := window.AnswerSources[source]; known {
						window.AnswerSources[source] += count
					}
				case "UNMATCHED", "AMBIGUOUS", "CLOSED_CARD_REPLY":
					window.UnusableAnswers += count
				case "REPEATED_INPUT":
					window.RepeatedInput += count
				case "REPLY_REPAIRED":
					switch decision {
					case "EMPTY", "BROKEN_TEXT", "INTERNAL_CODE":
						window.BrokenReplies += count
					case "TELEGRAM_MECHANICS":
						window.MechanicsReplies += count
					}
				}
			}
			return rows.Err()
		}),
	)
	current.finalize()
	previous.finalize()
	summary.Current, summary.Previous = current, previous
	summary.DegradedSections = degraded
	summary.NotYetMeasurable = append(summary.NotYetMeasurable, "answers_via_assistant")
	return summary, nil
}

func (h *Handler) InteractionWeekly(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.HasHousehold {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	if principal.HouseholdRole != "OWNER" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "owner role required"})
		return
	}
	budget := h.statusTimeout
	if budget == 0 {
		budget = interactionBudget
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	summary, err := h.loadInteractionWeekly(ctx, principal.HouseholdID, time.Now())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "interaction summary unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
