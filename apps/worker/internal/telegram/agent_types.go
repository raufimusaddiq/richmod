package telegram

import (
	"context"
	"encoding/json"
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
	ModelCallTimeout        time.Duration
	TotalTurnTimeout        time.Duration
}

const TextJobBudget = 160 * time.Second

const ProgressNoticeDelay = 10 * time.Second

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

func (r *agentToolResult) UnmarshalJSON(data []byte) error {
	type alias agentToolResult
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Review != nil {
		if raw, ok := decoded.Review["missing_fields"].([]any); ok {
			fields := make([]string, 0, len(raw))
			for _, item := range raw {
				if text, ok := item.(string); ok {
					fields = append(fields, text)
				}
			}
			decoded.Review["missing_fields"] = fields
		}
	}
	*r = agentToolResult(decoded)
	return nil
}

type agentPublicRef struct {
	Ref   string `json:"ref"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

type agentState struct {
	SourceEventID      string
	HouseholdID        string
	Update             telegramUpdate
	Now                time.Time
	Categories         []string
	Tools              []gateway.ToolDefinition
	RequiredTool       string
	TurnContext        map[string]any
	History            []agentToolResult
	ModelPhases        int
	ArgumentRepairs    int
	ReadCalls          int
	SideEffects        int
	Recoveries         int
	ReplyQueued        bool
	ReadCache          map[string]agentToolResult
	activity           *atomic.Int32
	HasPendingAction   bool
	HasPendingBatch    bool
	HasSalaryChoice    bool
	ReviewType         string
	ReviewMode         string
	ResidualDimensions []string

	PreviousResponseID string
	PreviousToolCalls  []gateway.ToolCall
	PendingToolOutputs []gateway.AgentToolOutput
	ReadHistory        []gateway.AgentReadPhase

	ReviewBinding           *agentReviewBinding
	ReviewBindingCount      int
	MerchantLearningBinding *agentMerchantLearningBinding
	MerchantLearningCount   int

	Analytics *analyticscore.Session

	FreshEvidenceDocuments []string

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
