package financialemail

import (
	"context"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/reviewdomain/reviewdec"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

type ObservationClassification struct {
	ObservationType              string
	TypeAccepted                 bool
	MovementType                 string
	MovementAccepted             bool
	CashSupported                bool
	WealthSupported              bool
	EvidenceSufficient           bool
	MaterialAmbiguity            bool
	ClaimOutcomes                map[string]string
	AmbiguityDecidedNotAmbiguous bool

	Model         string
	PolicyVersion string
}

const ProviderEmailClassificationPolicyVersion = "2026-09-jev4"

type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

var observationTypes = []string{"CASH_MOVEMENT", "WEALTH_VALUE", "NON_ACTIONABLE"}

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
		"evidence_sufficient":     {Type: "noul", Instructions: "Does the email support the extracted amount or value and, for cash movements, transaction time? Do not require account hints: Go resolves canonical accounts independently."},
		"material_ambiguity":      {Type: "noul", Instructions: "Is this email genuinely ambiguous, for example two plausible amounts, values, or targets?"},
	}
	if strings.EqualFold(v.Kind, "CASH_MOVEMENT") || pointerValue(v.MovementType) != "" {
		questions["movement_type"] = judgment.Question{Type: "choice", Instructions: "Choose the movement this email describes. Use OTHER_OR_UNCLEAR when the wording does not support one.", Criteria: judgment.ChoiceCriteria(movementTypeCriteria)}
	}
	ctx = judgment.WithPhaseMetadata(ctx, "EVIDENCE_SUPPORT", ProviderEmailClassificationPolicyVersion, []string{})
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
	classification.ClaimOutcomes = map[string]string{}
	for _, key := range []string{"observation_type", "movement_type", "cash_movement_supported", "wealth_value_supported", "evidence_sufficient", "material_ambiguity"} {
		answer, ok := result.Answers[key]
		if !ok {
			classification.ClaimOutcomes[key] = "UNDECIDED"
			continue
		}
		if answer.Type == "choice" {
			criteria, policy := observationTypeCriteria, classificationPolicy.Type
			if key == "movement_type" {
				criteria, policy = movementTypeCriteria, classificationPolicy.Movement
			}
			classification.ClaimOutcomes[key] = "UNDECIDED"
			if judgment.AcceptChoice(answer, judgment.ChoiceCriteria(criteria), policy) {
				classification.ClaimOutcomes[key] = answer.Choice
			}
			continue
		}
		policy := classificationPolicy.Supported
		if key == "material_ambiguity" {
			policy = classificationPolicy.Ambiguity
		}
		yes, decided := judgment.AcceptNoul(answer, policy)
		classification.ClaimOutcomes[key] = "UNDECIDED"
		if decided {
			classification.ClaimOutcomes[key] = "NO"
			if yes {
				classification.ClaimOutcomes[key] = "YES"
			}
		}
	}
	return classification, true, nil
}

func (c ObservationClassification) cashResidual() []string {
	var missing []string
	c.eachFailedClaim(func(fact string) { missing = append(missing, fact) })
	return missing
}

var cashClaimFacts = []struct{ key, fact, expected string }{
	{"observation_type", "observation_type", "CASH_MOVEMENT"},
	{"cash_movement_supported", "cash_movement", "YES"},
	{"evidence_sufficient", "evidence_support", "YES"},
	{"material_ambiguity", "transaction_ambiguity", "NO"},
}

func (c ObservationClassification) eachFailedClaim(report func(fact string)) {
	for _, claim := range cashClaimFacts {
		if c.ClaimOutcomes[claim.key] != claim.expected {
			report(claim.fact)
		}
	}
}

func (c ObservationClassification) reviewFacts(residual []string) (missing, affected []string, consequence reviewdec.Consequence, provenance map[string]any) {
	missing = residual
	if len(missing) == 0 {
		c.eachFailedClaim(func(fact string) { missing = append(missing, fact) })
		if len(missing) == 0 {
			missing = []string{"evidence_support"}
		}
	}
	affected = append([]string(nil), missing...)
	consequence = reviewdec.BoundedResidual
	if containsString(missing, "transaction_ambiguity") {
		consequence = reviewdec.CanonicalAmbiguity
	}
	provenance = map[string]any{"pipeline": "financial-provider-email"}
	if c.ClaimOutcomes == nil {
		provenance["claim_outcomes"] = map[string]string{}
	} else {
		provenance["claim_outcomes"] = c.ClaimOutcomes
	}
	return
}

func (c ObservationClassification) cashAllowed() bool {
	return c.TypeAccepted && c.ObservationType == "CASH_MOVEMENT" && c.CashSupported && c.EvidenceSufficient && c.AmbiguityDecidedNotAmbiguous &&
		c.MovementAccepted && c.MovementType != ""
}

func noulSupported(answers map[string]judgment.Answer, key string) bool {
	answer, ok := answers[key]
	if !ok {
		return false
	}
	supported, decided := judgment.AcceptNoul(answer, classificationPolicy.Supported)
	return supported && decided
}

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
