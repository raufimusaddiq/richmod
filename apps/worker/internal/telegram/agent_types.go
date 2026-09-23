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
	MaxReadCallsPerResponse: 5,
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
	SourceEventID    string
	HouseholdID      string
	Update           telegramUpdate
	Now              time.Time
	Categories       []string
	Tools            []gateway.ToolDefinition
	RequiredTool     string
	TurnContext      map[string]any
	History          []agentToolResult
	ModelPhases      int
	ReadCalls        int
	SideEffects      int
	HasPendingAction bool
	HasPendingBatch  bool
	HasSalaryChoice  bool
	ReviewMode       string
	// WorkflowScope is the server-owned target precedence chosen for this turn
	// (see applyAgentWorkflowToolPolicy). It lets a bound-workflow handler tell an
	// explicit user reply to a workflow apart from an implicit chat-level binding.
	WorkflowScope string
	// GeneralTools is the unfiltered side-effect catalog, retained so an implicit
	// binding that turns out not to match the message can fall through to normal
	// handling instead of swallowing the turn.
	GeneralTools []gateway.ToolDefinition

	// Native continuation state for the immediately preceding READ phase. The
	// gateway consumes these as provider-native tool outputs on the next model
	// phase, preserving call IDs and ordering rather than pretending tool output
	// is a new user message.
	PreviousResponseID string
	PreviousToolCalls  []gateway.ToolCall
	PendingToolOutputs []gateway.AgentToolOutput

	// Review and merchant-learning targets are resolved by Go before the model
	// turn. These server-only bindings are never exposed as canonical IDs to the
	// model and prevent a later "latest row wins" query from changing targets.
	ReviewBinding           *agentReviewBinding
	ReviewBindingCount      int
	MerchantLearningBinding *agentMerchantLearningBinding
	MerchantLearningCount   int
}
