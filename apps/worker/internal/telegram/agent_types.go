package telegram

import (
	"context"
	"time"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type conversationalGateway interface {
	AgentTurn(ctx context.Context, requestID string, request gateway.AgentRequest) (gateway.AgentResponse, error)
}

type agentLimits struct {
	MaxModelPhases          int
	MaxReadCallsPerResponse int
	MaxReadCallsPerTurn     int
	MaxSideEffectsPerTurn   int
	PerModelCallTimeout     time.Duration
	TotalTurnTimeout        time.Duration
}

var defaultAgentLimits = agentLimits{
	MaxModelPhases:          5,
	MaxReadCallsPerResponse: 4,
	MaxReadCallsPerTurn:     8,
	MaxSideEffectsPerTurn:   1,
	PerModelCallTimeout:     8 * time.Second,
	TotalTurnTimeout:        20 * time.Second,
}

type agentToolResult struct {
	CallID     string           `json:"call_id,omitempty"`
	Tool       string           `json:"tool"`
	Class      agentToolClass   `json:"class"`
	Status     string           `json:"status"`
	Facts      map[string]any   `json:"facts,omitempty"`
	References []agentPublicRef `json:"references,omitempty"`
	Mutation   map[string]any   `json:"mutation,omitempty"`
	Review     map[string]any   `json:"review,omitempty"`
}

type agentPublicRef struct {
	Ref   string `json:"ref"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

type agentState struct {
	SourceEventID string
	HouseholdID   string
	Update        telegramUpdate
	Now           time.Time
	Categories    []string
	Tools         []gateway.ToolDefinition
	TurnContext   map[string]any
	History       []agentToolResult
	ModelPhases   int
	ReadCalls     int
	SideEffects   int

	// Review and merchant-learning targets are resolved by Go before the model
	// turn. These server-only bindings are never exposed as canonical IDs to the
	// model and prevent a later "latest row wins" query from changing targets.
	ReviewBinding           *agentReviewBinding
	ReviewBindingCount      int
	MerchantLearningBinding *agentMerchantLearningBinding
	MerchantLearningCount   int
}
