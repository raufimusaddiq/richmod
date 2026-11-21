package telegram

import "context"

type turnIdentityKey struct{}

type turnIdentity struct{ householdID, sourceEventID string }

func withTurnIdentity(ctx context.Context, householdID, sourceEventID string) context.Context {
	return context.WithValue(ctx, turnIdentityKey{}, turnIdentity{householdID, sourceEventID})
}

func TurnHouseholdID(ctx context.Context) string {
	identity, _ := ctx.Value(turnIdentityKey{}).(turnIdentity)
	return identity.householdID
}

func TurnSourceEventID(ctx context.Context) string {
	identity, _ := ctx.Value(turnIdentityKey{}).(turnIdentity)
	return identity.sourceEventID
}

func WithHousehold(ctx context.Context, householdID string) context.Context {
	if householdID == "" || TurnHouseholdID(ctx) != "" {
		return ctx
	}
	return withTurnIdentity(ctx, householdID, "")
}
