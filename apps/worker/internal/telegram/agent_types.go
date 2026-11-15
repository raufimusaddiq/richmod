package telegram

import (
	"context"
	"sync/atomic"
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
	// ModelCallTimeout is one cap on every model call in a turn; it bounds a hung call, not a slow one.
	ModelCallTimeout time.Duration
	// TotalTurnTimeout is a backstop, not the working limit: it must cover every phase at the call cap.
	TotalTurnTimeout time.Duration
}

// TextJobBudget is the queue budget for one PROCESS_TELEGRAM_TEXT attempt; it must stay above
// TotalTurnTimeout and below the five-minute job lease.
const TextJobBudget = 160 * time.Second

// ProgressNoticeDelay is how long a turn runs before the household is told the answer is still coming;
// single-phase turns (p90 about 6 s) finish inside it.
const ProgressNoticeDelay = 10 * time.Second

// MaxModelTimeoutAttempts bounds retries of a model timeout; a retry uses the same prompt and cap, so more
// attempts only delay the answer.
const MaxModelTimeoutAttempts = 2

var defaultAgentLimits = agentLimits{
	MaxModelPhases:          5,
	MaxReadCallsPerResponse: 5,
	MaxReadCallsPerTurn:     8,
	MaxSideEffectsPerTurn:   1,
	ModelCallTimeout:        25 * time.Second,
	TotalTurnTimeout:        130 * time.Second,
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

	confirmationToken string
}

type agentPublicRef struct {
	Ref   string `json:"ref"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

type agentState struct {
	SourceEventID   string
	HouseholdID     string
	Update          telegramUpdate
	Now             time.Time
	Categories      []string
	Tools           []gateway.ToolDefinition
	RequiredTool    string
	TurnContext     map[string]any
	History         []agentToolResult
	ModelPhases     int
	ArgumentRepairs int
	ReadCalls       int
	SideEffects     int
	Recoveries      int
	// ReplyQueued is set when the reply was queued inside the mutation's own transaction, so the loop must
	// not answer again.
	ReplyQueued bool
	ReadCache   map[string]agentToolResult
	// activity is read by the progress notice from its own goroutine; a pointer so copying the state shares
	// it instead of copying an atomic.
	activity         *atomic.Int32
	HasPendingAction bool
	HasPendingBatch  bool
	HasSalaryChoice  bool
	// ReviewType is the semantic review kind, distinct from ReviewMode (the bound canonical subject/executor).
	ReviewType string
	ReviewMode string
	// ResidualDimensions names the facts left unresolved after generative extraction so provenance records
	// what the rescue owned (ADR-045); empty means direct acceptance.
	ResidualDimensions []string

	// Native continuation state for the preceding READ phase, consumed as provider-native tool outputs to
	// keep call IDs and ordering.
	PreviousResponseID string
	PreviousToolCalls  []gateway.ToolCall
	PendingToolOutputs []gateway.AgentToolOutput

	// Server-only review and merchant-learning targets resolved by Go before the model turn; never exposed as
	// canonical IDs. They stop a later "latest row wins" query from changing targets.
	ReviewBinding           *agentReviewBinding
	ReviewBindingCount      int
	MerchantLearningBinding *agentMerchantLearningBinding
	MerchantLearningCount   int

	// Analytics is the request-scoped READ session over the analytics API's fact engine; it never mutates.
	Analytics *analyticscore.Session

	// FreshEvidenceDocuments are canonical ids of documents sent in the immediate window; server-only, so the
	// ledger guard rather than the model refuses a duplicate row.
	FreshEvidenceDocuments []string

	// ReceivedAt is when the message reached Richmod; a message deferred for an unavailable gateway gives up
	// after gatewayDeferralWindow.
	ReceivedAt time.Time
}

func (s *agentState) setActivity(activity agentActivity) {
	if s.activity == nil {
		s.activity = new(atomic.Int32)
	}
	s.activity.Store(int32(activity))
}

func (s *agentState) activityNow() agentActivity {
	if s.activity == nil {
		return activityThinking
	}
	return agentActivity(s.activity.Load())
}
