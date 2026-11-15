package telegram

import "context"

type turnIdentityKey struct{}

type turnIdentity struct{ householdID, sourceEventID string }

// withTurnIdentity attaches household and source event to the context so the worker's LLM-call recorder can
// attribute every model call.
func withTurnIdentity(ctx context.Context, householdID, sourceEventID string) context.Context {
	return context.WithValue(ctx, turnIdentityKey{}, turnIdentity{householdID, sourceEventID})
}

// TurnHouseholdID returns the turn's household, or "" outside a turn.
func TurnHouseholdID(ctx context.Context) string {
	identity, _ := ctx.Value(turnIdentityKey{}).(turnIdentity)
	return identity.householdID
}

func TurnSourceEventID(ctx context.Context) string {
	identity, _ := ctx.Value(turnIdentityKey{}).(turnIdentity)
	return identity.sourceEventID
}

// WithHousehold attributes non-Telegram work (document, email, insight, residual jobs) to its household; a
// later Telegram turn context replaces it.
func WithHousehold(ctx context.Context, householdID string) context.Context {
	if householdID == "" || TurnHouseholdID(ctx) != "" {
		return ctx
	}
	return withTurnIdentity(ctx, householdID, "")
}
