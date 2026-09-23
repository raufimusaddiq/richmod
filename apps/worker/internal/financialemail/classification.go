package financialemail

import (
	"context"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// ObservationClassification is the bounded ruling over one already-extracted
// provider-email observation. It replaces the generative self-confidence gate:
// the extractor produces arbitrary facts, this object says whether the evidence
// supports the semantic claims those facts depend on (PRD §21).
type ObservationClassification struct {
	ObservationType    string
	TypeAccepted       bool
	MovementType       string
	MovementAccepted   bool
	CashSupported      bool
	WealthSupported    bool
	EvidenceSufficient bool
	MaterialAmbiguity  bool
	// AmbiguityDecidedNotAmbiguous records a decided *negative* on the ambiguous
	// question, which is the favourable answer. The middle band means the plane
	// could not tell, and must fail closed rather than read as approval.
	AmbiguityDecidedNotAmbiguous bool

	Model         string
	PolicyVersion string
}

// ProviderEmailClassificationPolicyVersion marks the thresholds behind these
// rulings so a stored decision stays reproducible (PRD §18).
const ProviderEmailClassificationPolicyVersion = "2026-09-jev3"

// jeverifier is the seam onto the bounded judgment plane, expressed in the terms
// this package needs so provider email never imports Telegram policy.
type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

// observationTypes is the canonical possibility space for one observation.
var observationTypes = []string{"CASH_MOVEMENT", "WEALTH_VALUE", "NON_ACTIONABLE"}

// observationTypeCriteria describes each kind for the model. Capabilities only
// restrict the tool schema; this Choice is what actually rules on the semantics.
var observationTypeCriteria = map[string]string{
	"CASH_MOVEMENT":    "a real movement of money into, out of, or between the household's accounts",
	"WEALTH_VALUE":     "an observed current value of a holding, not a movement of money",
	"NON_ACTIONABLE":   "a notice that does not describe a financial movement or value",
	"OTHER_OR_UNCLEAR": "the email does not support a safe observation type",
}

var movementTypes = []string{"CONTRIBUTION", "WITHDRAWAL", "ASSET_PURCHASE"}

var movementTypeCriteria = map[string]string{
	"CONTRIBUTION":     "money moved into the provider account",
	"WITHDRAWAL":       "money redeemed or moved out of the provider account",
	"ASSET_PURCHASE":   "money used inside the provider to buy an asset",
	"OTHER_OR_UNCLEAR": "the email does not support a safe movement type",
}

var classificationPolicy = struct {
	Type      judgment.ChoicePolicy
	Movement  judgment.ChoicePolicy
	Supported judgment.NoulPolicy
	Ambiguity judgment.NoulPolicy
}{
	Type:      judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60},
	Movement:  judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60},
	Supported: judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Ambiguity: judgment.NoulPolicy{High: 0.15, Low: 0.05},
}

// classifyObservation asks one bounded bundle about one extracted observation.
// The provider never generates merchant/date strings here; it rules on claims Go
// already holds. `verified=false` means no verifier is configured, and callers
// must keep their deterministic structural checks in that case.
func (p *Processor) classifyObservation(ctx context.Context, requestID string, v observation) (ObservationClassification, bool, error) {
	if p.verifier == nil {
		return ObservationClassification{}, false, nil
	}
	state := map[string]any{
		"allowed_observation_types": observationTypes,
		"allowed_movement_types":    movementTypes,
		"extracted": map[string]any{
			"kind":          v.Kind,
			"movement_type": pointerValue(v.MovementType),
			"amount_idr":    pointerValue(v.AmountIDR),
			"value_idr":     pointerValue(v.ValueIDR),
			"has_reference": strings.TrimSpace(pointerValue(v.ProviderReference)) != "",
		},
	}
	questions := map[string]judgment.Question{
		"observation_type":        {Type: "choice", Instructions: "Choose what this provider email actually describes. Use OTHER_OR_UNCLEAR when the evidence is not enough to decide.", Criteria: judgment.ChoiceCriteria(observationTypeCriteria)},
		"cash_movement_supported": {Type: "noul", Instructions: "Does the email directly support that real money moved, rather than describing a value change or a notice?"},
		"wealth_value_supported":  {Type: "noul", Instructions: "Does the email directly state a current holding value?"},
		"evidence_sufficient":     {Type: "noul", Instructions: "Are the extracted amount or value, time, and account hints fully supported by the email text?"},
		"material_ambiguity":      {Type: "noul", Instructions: "Is this email genuinely ambiguous, for example two plausible amounts, values, or targets?"},
	}
	if strings.EqualFold(v.Kind, "CASH_MOVEMENT") || pointerValue(v.MovementType) != "" {
		questions["movement_type"] = judgment.Question{Type: "choice", Instructions: "Choose the movement this email describes. Use OTHER_OR_UNCLEAR when the wording does not support one.", Criteria: judgment.ChoiceCriteria(movementTypeCriteria)}
	}
	result, err := p.verifier.Evaluate(ctx, requestID, judgment.Request{State: state, Questions: questions})
	if err != nil {
		return ObservationClassification{}, false, err
	}
	classification := ObservationClassification{Model: result.Model, PolicyVersion: ProviderEmailClassificationPolicyVersion}
	if answer, ok := result.Answers["observation_type"]; ok {
		criteria := judgment.ChoiceCriteria(observationTypeCriteria)
		if judgment.AcceptChoice(answer, criteria, classificationPolicy.Type) && containsString(observationTypes, answer.Choice) {
			classification.ObservationType, classification.TypeAccepted = answer.Choice, true
		}
	}
	if answer, ok := result.Answers["movement_type"]; ok {
		criteria := judgment.ChoiceCriteria(movementTypeCriteria)
		if judgment.AcceptChoice(answer, criteria, classificationPolicy.Movement) && containsString(movementTypes, answer.Choice) {
			classification.MovementType, classification.MovementAccepted = answer.Choice, true
		}
	}
	classification.CashSupported = noulSupported(result.Answers, "cash_movement_supported")
	classification.WealthSupported = noulSupported(result.Answers, "wealth_value_supported")
	classification.EvidenceSufficient = noulSupported(result.Answers, "evidence_sufficient")
	classification.MaterialAmbiguity, classification.AmbiguityDecidedNotAmbiguous = ambiguityVerdict(result.Answers, "material_ambiguity")
	return classification, true, nil
}

// cashAllowed reports whether Go may treat this observation as a real cash
// movement. Every bounded claim must hold; anything undecided fails closed.
func (c ObservationClassification) cashAllowed() bool {
	return c.TypeAccepted && c.ObservationType == "CASH_MOVEMENT" && c.CashSupported && c.EvidenceSufficient && c.AmbiguityDecidedNotAmbiguous &&
		c.MovementAccepted && c.MovementType != ""
}

// wealthAllowed reports whether Go may treat this observation as a wealth value.
func (c ObservationClassification) wealthAllowed() bool {
	return c.TypeAccepted && c.ObservationType == "WEALTH_VALUE" && c.WealthSupported && c.EvidenceSufficient && c.AmbiguityDecidedNotAmbiguous
}

// nonActionable reports a decided NON_ACTIONABLE ruling, which is a terminal
// no-op rather than a review.
func (c ObservationClassification) nonActionable() bool {
	return c.TypeAccepted && c.ObservationType == "NON_ACTIONABLE"
}

// noulSupported reports a decided, affirmative Noul answer. The middle band
// fails closed, matching the Telegram decision plane.
func noulSupported(answers map[string]judgment.Answer, key string) bool {
	answer, ok := answers[key]
	if !ok {
		return false
	}
	supported, decided := judgment.AcceptNoul(answer, classificationPolicy.Supported)
	return supported && decided
}

// ambiguityVerdict reads the inverted claim. It returns (isAmbiguous,
// decidedNotAmbiguous) so a caller can require an affirmative not-ambiguous
// ruling instead of treating an undecided answer as approval.
func ambiguityVerdict(answers map[string]judgment.Answer, key string) (bool, bool) {
	answer, ok := answers[key]
	if !ok {
		return false, false
	}
	ambiguous, decided := judgment.AcceptNoul(answer, classificationPolicy.Ambiguity)
	return ambiguous, decided && !ambiguous
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
