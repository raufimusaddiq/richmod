// Package reviewdec holds the PRD §7 canonical ReviewDecision contract: the
// structured, bounded explanation of why a review exists. It is deliberately a
// plain value with JSON tags — the storage shape is a jsonb column on
// review_item — so every review-creating path can populate the same fields
// without a new table, service, or migration per source.
//
// It carries no canonical IDs to the model and performs no mutation; it only
// records what Go already decided so the Review Inbox can render the proposal,
// the missing fact, and the reason without re-deriving them (PRD §7, §13, §20).
package reviewdec

import "encoding/json"

// Version is the contract revision stored on every decision. Bump it when the
// meaning of a field changes, not when a new optional field is added.
const Version = 1

// Decision classes (PRD §6).
const (
	ClassEvidenceGap            = "EVIDENCE_GAP"
	ClassEvidenceConflict       = "EVIDENCE_CONFLICT"
	ClassDuplicateAmbiguity     = "DUPLICATE_AMBIGUITY"
	ClassHumanPolicyChoice      = "HUMAN_POLICY_CHOICE"
	ClassCorrectionConfirmation = "CORRECTION_CONFIRMATION"
)

// Interaction modes (PRD §7.8). Generic FORM is intentionally absent so it
// cannot become the default by accident.
const (
	ModeOneTapConfirmation = "ONE_TAP_CONFIRMATION"
	ModeBoundedChoice      = "BOUNDED_CHOICE"
	ModeSingleField        = "SINGLE_FIELD"
	ModeConflictResolution = "CONFLICT_RESOLUTION"
	ModePolicyChoice       = "POLICY_CHOICE"
)

// Decision sources (PRD §7.6).
const (
	SourceDeterministic        = "DETERMINISTIC"
	SourceJev                  = "JEV"
	SourceGenerativeExtraction = "GENERATIVE_EXTRACTION"
	SourceDeterministicPlusJev = "DETERMINISTIC_PLUS_JEV"
	SourceGenerativePlusJev    = "GENERATIVE_PLUS_JEV"
	SourceUserPolicy           = "USER_POLICY"
)

// Subject identifies the canonical thing the review concerns. Type is a stable
// discriminator such as "source_event"; ID is a canonical UUID that stays on the
// server and is never handed to a model as a choice.
type Subject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Choice is one server-owned bounded option. Key is what Go maps back to a
// canonical value; Label is what the user reads.
type Choice struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// EvidenceRef names where a fact came from without storing the payload.
type EvidenceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// Decision is the stored contract. Facts use json.RawMessage so a caller can put
// its own bounded shape in known/proposed/conflicting without this package
// knowing each domain's fields. Missing and Conflicting stay explicit and
// separate from Known so a UI can request only the unresolved dimensions.
type Decision struct {
	Version         int                        `json:"version"`
	Subject         Subject                    `json:"subject"`
	SourceEventID   string                     `json:"sourceEventId,omitempty"`
	ReasonCode      string                     `json:"reasonCode"`
	DecisionClass   string                     `json:"decisionClass"`
	KnownFacts      map[string]any             `json:"knownFacts"`
	ProposedFacts   map[string]any             `json:"proposedFacts,omitempty"`
	MissingFacts    []string                   `json:"missingFacts"`
	Conflicting     map[string][]EvidenceValue `json:"conflictingFacts,omitempty"`
	Choices         []Choice                   `json:"boundedChoices,omitempty"`
	EvidenceRefs    []EvidenceRef              `json:"evidenceRefs,omitempty"`
	DecisionSource  string                     `json:"decisionSource"`
	PolicyVersion   string                     `json:"decisionPolicyVersion"`
	Provenance      map[string]any             `json:"decisionProvenance,omitempty"`
	WhyNotAuto      string                     `json:"whyNotAutoConfirm"`
	AllowedActions  []string                   `json:"allowedActions"`
	InteractionMode string                     `json:"interactionMode"`
}

// EvidenceValue is one supported value plus the evidence that supports it, used
// when two pieces of evidence disagree (PRD §7.4).
type EvidenceValue struct {
	Value    string `json:"value"`
	Evidence string `json:"evidence"`
}

// JSON marshals the decision for storage in review_item.decision. MissingFacts
// and KnownFacts are normalized to non-nil so the stored shape always contains
// the keys the UI reads, which keeps rendering branch-free.
func (d Decision) JSON() ([]byte, error) {
	if d.Version == 0 {
		d.Version = Version
	}
	if d.KnownFacts == nil {
		d.KnownFacts = map[string]any{}
	}
	if d.MissingFacts == nil {
		d.MissingFacts = []string{}
	}
	if d.AllowedActions == nil {
		d.AllowedActions = []string{}
	}
	if d.Provenance == nil {
		d.Provenance = map[string]any{}
	}
	return json.Marshal(d)
}
