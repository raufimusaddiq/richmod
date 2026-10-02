package telegram

import (
	"context"
	"time"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/analyticscore"
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
	// PerModelCallTimeout bounds a model call that chooses tools or answers a
	// simple message (measured p50 about 3.5 s, p90 about 6 s).
	PerModelCallTimeout time.Duration
	// AnswerPhaseTimeout bounds a model call that writes the answer from tool
	// results, such as an analytics explanation. Long-form output on the same
	// models takes 9 to 11 s, so the 8 s cap made it fail every time.
	AnswerPhaseTimeout time.Duration
	TotalTurnTimeout   time.Duration
}

// TextJobBudget is the queue budget for one PROCESS_TELEGRAM_TEXT attempt: the
// turn timeout plus room for reads, writes, and the reply. It must stay above
// TotalTurnTimeout, and well below the five-minute job lease.
const TextJobBudget = 50 * time.Second

// MaxModelTimeoutAttempts is how many times a typed message is tried when the
// model call times out. A timeout repeats with the same prompt and the same cap,
// so more attempts only delay the answer the user is waiting for.
const MaxModelTimeoutAttempts = 2

var defaultAgentLimits = agentLimits{
	MaxModelPhases:          5,
	MaxReadCallsPerResponse: 5,
	MaxReadCallsPerTurn:     8,
	MaxSideEffectsPerTurn:   1,
	PerModelCallTimeout:     8 * time.Second,
	AnswerPhaseTimeout:      25 * time.Second,
	TotalTurnTimeout:        45 * time.Second,
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
	Route            string
	TurnContext      map[string]any
	History          []agentToolResult
	ModelPhases      int
	ReadCalls        int
	SideEffects      int
	HasPendingAction bool
	HasPendingBatch  bool
	HasSalaryChoice  bool
	// ReviewType is the semantic review kind (server-owned action vocabulary),
	// distinct from ReviewMode, which is the bound canonical subject/executor.
	ReviewType string
	ReviewMode string
	// ResidualDimensions names the bounded facts Go sent to Jev after a
	// generative extraction, so provenance records what the rescue actually owned
	// instead of claiming the model re-decided a complete transaction (ADR-045
	// telemetry). Empty means a direct acceptance with no bounded rescue.
	ResidualDimensions []string

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

	// Analytics is the request-scoped shared cycle-review READ session over the
	// same deterministic fact engine as the analytics API. It never mutates.
	Analytics *analyticscore.Session
}
