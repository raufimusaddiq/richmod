package reviewdec

import (
	"encoding/json"
	"errors"
)

const Version = 1

const (
	ClassEvidenceGap            = "EVIDENCE_GAP"
	ClassEvidenceConflict       = "EVIDENCE_CONFLICT"
	ClassDuplicateAmbiguity     = "DUPLICATE_AMBIGUITY"
	ClassHumanPolicyChoice      = "HUMAN_POLICY_CHOICE"
	ClassCorrectionConfirmation = "CORRECTION_CONFIRMATION"
)

type Consequence string

const (
	HardCanonicalInvariant      Consequence = "HARD_CANONICAL_INVARIANT"
	RepresentationInvalid       Consequence = "REPRESENTATION_INVALID"
	IndependentEvidenceConflict Consequence = "INDEPENDENT_EVIDENCE_CONFLICT"
	BoundedResidual             Consequence = "BOUNDED_RESIDUAL"
	QualitySignal               Consequence = "QUALITY_SIGNAL"
	HumanPolicy                 Consequence = "HUMAN_POLICY"
	CanonicalAmbiguity          Consequence = "CANONICAL_AMBIGUITY"
)

const (
	ModeOneTapConfirmation = "ONE_TAP_CONFIRMATION"
	ModeBoundedChoice      = "BOUNDED_CHOICE"
	ModeSingleField        = "SINGLE_FIELD"
	ModeConflictResolution = "CONFLICT_RESOLUTION"
	ModePolicyChoice       = "POLICY_CHOICE"
)

const (
	SourceDeterministic        = "DETERMINISTIC"
	SourceGenerativeExtraction = "GENERATIVE_EXTRACTION"
	SourceUserPolicy           = "USER_POLICY"
)

type Subject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Choice struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type EvidenceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

type Decision struct {
	Version         int                        `json:"version"`
	Subject         Subject                    `json:"subject"`
	SourceEventID   string                     `json:"sourceEventId,omitempty"`
	ReasonCode      string                     `json:"reasonCode"`
	Consequence     Consequence                `json:"validationConsequence,omitempty"`
	AffectedFacts   []string                   `json:"affectedFacts,omitempty"`
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

type EvidenceValue struct {
	Value    string `json:"value"`
	Evidence string `json:"evidence"`
}

func (d Decision) Validate() error {
	if d.ReasonCode == "" {
		return errors.New("review decision: reasonCode is required")
	}
	if len(d.AllowedActions) == 0 {
		return errors.New("review decision: allowedActions must not be empty")
	}
	return nil
}

func (d Decision) JSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.Version == 0 {
		d.Version = Version
	}
	if d.KnownFacts == nil {
		d.KnownFacts = map[string]any{}
	}
	if d.ProposedFacts == nil {
		d.ProposedFacts = map[string]any{}
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
