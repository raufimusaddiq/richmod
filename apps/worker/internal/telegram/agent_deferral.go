package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Durable deferral while the AI gateway is unavailable (ADR-052 amendment,
// 2026-11). A typed message that needs the conversational model is never
// turned into a transaction by a fallback parser: no word list can tell a
// completed expense from a plan, a price or a request, and a guessed record is
// worse than a late one. Instead the message — already durable as its source
// event — waits: the household is told once that it was saved and nothing was
// recorded yet, the job is set aside with a growing backoff that spends no
// attempts (queue.Defer), and when the gateway is back the message runs the
// full agent path, so bound reviews and pending workflows are handled as usual.
// After gatewayDeferralWindow the message gives up with an honest "send it
// again" notice and the job ends FAILED, visible in operations.

// gatewayDeferralWindow is how long a message waits for the gateway.
const gatewayDeferralWindow = 24 * time.Hour

// gatewayDeferredMessage is sent once per message when it is first deferred.
const gatewayDeferredMessage = "AI Richmod sedang tidak bisa dihubungi. Pesanmu sudah kusimpan dan akan kuproses otomatis begitu tersambung lagi — belum ada yang dicatat."

// gatewayDeferredTool names the bookkeeping turn row that records the saved
// notice was sent; it is never replayed to the model.
const gatewayDeferredTool = "agent_gateway_deferred"

// GatewayDeferral asks the worker to set the job aside (queue.Defer) instead of
// failing it.
type GatewayDeferral struct{ Err error }

func (e *GatewayDeferral) Error() string {
	return "deferred until the AI gateway is back: " + e.Err.Error()
}
func (e *GatewayDeferral) Unwrap() error { return e.Err }

// IsGatewayDeferral reports whether a typed-message failure should wait for the
// gateway rather than spend a retry.
func IsGatewayDeferral(err error) bool {
	var deferral *GatewayDeferral
	return errors.As(err, &deferral)
}

// deferForGateway handles a turn whose model call found the gateway
// unavailable before anything was written. Inside the window it sends the
// saved notice once and returns a GatewayDeferral; past it, it returns a
// terminal unavailability so the job fails with the give-up notice.
func (p *Processor) deferForGateway(ctx context.Context, state *agentState, cause error) error {
	failure := fmt.Errorf("%w: %w", errModelPhase, cause)
	if !state.ReceivedAt.IsZero() && p.now().Sub(state.ReceivedAt) >= gatewayDeferralWindow {
		return TerminalError(failure)
	}
	tx, err := p.pool.Begin(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, "telegram_deferral:"+state.SourceEventID); err != nil {
		return err
	}
	var noticed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_conversation_turn WHERE source_event_id=$1::uuid AND role='TOOL' AND tool_name=$2::text)`, state.SourceEventID, gatewayDeferredTool).Scan(&noticed); err != nil {
		return err
	}
	if !noticed {
		if err := enqueueReply(ctx, tx, state.Update, gatewayDeferredMessage); err != nil {
			return err
		}
		if err := insertTurnTx(ctx, tx, state.HouseholdID, state.SourceEventID, state.Update, "TOOL", "", gatewayDeferredTool, map[string]any{"deferred_at": p.now().Format(time.RFC3339)}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return &GatewayDeferral{Err: failure}
}
