package telegram

import "context"

type turnIdentityKey struct{}

type turnIdentity struct{ householdID, sourceEventID string }

// withTurnIdentity attaches the household and source event of a Telegram turn to
// its context. The worker's process-wide LLM-call recorder reads it so every
// model call is attributable without the gateway knowing about turns.
func withTurnIdentity(ctx context.Context, householdID, sourceEventID string) context.Context {
	return context.WithValue(ctx, turnIdentityKey{}, turnIdentity{householdID, sourceEventID})
}

// TurnHouseholdID returns the household of the current Telegram turn, or "" for
// callers that are not a turn (tests, background jobs).
func TurnHouseholdID(ctx context.Context) string {
	identity, _ := ctx.Value(turnIdentityKey{}).(turnIdentity)
	return identity.householdID
}

// TurnSourceEventID returns the source event of the current Telegram turn.
func TurnSourceEventID(ctx context.Context) string {
	identity, _ := ctx.Value(turnIdentityKey{}).(turnIdentity)
	return identity.sourceEventID
}
