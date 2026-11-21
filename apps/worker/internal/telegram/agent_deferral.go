package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const gatewayDeferralWindow = 24 * time.Hour

const gatewayDeferredMessage = "AI Richmod sedang tidak bisa dihubungi. Pesanmu sudah kusimpan dan akan kuproses otomatis begitu tersambung lagi — belum ada yang dicatat."

const gatewayDeferredTool = "agent_gateway_deferred"

type GatewayDeferral struct{ Err error }

func (e *GatewayDeferral) Error() string {
	return "deferred until the AI gateway is back: " + e.Err.Error()
}
func (e *GatewayDeferral) Unwrap() error { return e.Err }

func IsGatewayDeferral(err error) bool {
	var deferral *GatewayDeferral
	return errors.As(err, &deferral)
}

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
