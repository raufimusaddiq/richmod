package insight

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/judgment"
)

// signalSelection is the bounded ruling over already-computed aggregates: does
// this household's data contain anything semantically worth narrating, and if so
// which family? Go has the numbers; deciding whether they *matter* is a bounded
// judgment, and prose generation is a separate capability (PRD §23).
type signalSelection struct {
	Family string

	SpendingChange        bool
	CategoryShift         bool
	MerchantConcentration bool
	CashflowPattern       bool
	SavingsPattern        bool
	MaterialAmbiguity     bool

	Model         string
	PolicyVersion string
}

// noteworthy reports whether the generative model should be asked for prose.
// Every claim must hold affirmatively; an undecided middle band fails closed to
// the deterministic response, which is both cheaper and safer than narrating
// nothing.
func (s signalSelection) noteworthy() bool {
	return s.Family != "" && s.Family != "NONE" && !s.MaterialAmbiguity &&
		(s.SpendingChange || s.CategoryShift || s.MerchantConcentration || s.CashflowPattern || s.SavingsPattern)
}

// InsightSignalPolicyVersion marks the thresholds behind these rulings so a
// stored insight stays reproducible (PRD §18).
const InsightSignalPolicyVersion = "2026-09-jev2"

// signalFamilies is the canonical possibility space. NONE means "nothing worth
// narrating", which is a real, expected outcome rather than a failure.
var signalFamilies = []string{"SPENDING_CHANGE", "CATEGORY_SHIFT", "MERCHANT_CONCENTRATION", "CASHFLOW_PATTERN", "SAVINGS_PATTERN", "NONE"}

var signalFamilyCriteria = map[string]string{
	"SPENDING_CHANGE":        "spending rose or fell materially versus the comparison period",
	"CATEGORY_SHIFT":         "the mix of spending categories changed materially",
	"MERCHANT_CONCENTRATION": "spending is concentrated in one merchant or vendor",
	"CASHFLOW_PATTERN":       "income versus outflow changed materially",
	"SAVINGS_PATTERN":        "savings allocation or unallocated surplus is notable",
	"NONE":                   "the numbers are ordinary and nothing is worth narrating",
	"OTHER_OR_UNCLEAR":       "the supplied facts do not support a safe selection",
}

var signalPolicy = struct {
	Family    judgment.ChoicePolicy
	Signal    judgment.NoulPolicy
	Ambiguity judgment.NoulPolicy
}{
	Family:    judgment.ChoicePolicy{MinTop: 0.85, MinMargin: 0.20, MinConfidence: 0.60},
	Signal:    judgment.NoulPolicy{High: 0.85, Low: 0.15},
	Ambiguity: judgment.NoulPolicy{High: 0.15, Low: 0.05},
}

// jeverifier is the seam onto the bounded judgment plane, expressed in this
// package's terms.
type jeverifier interface {
	Evaluate(context.Context, string, judgment.Request) (judgment.Result, error)
}

// selectSignal asks one bounded bundle over the deterministic aggregates. The
// facts are the same JSON the prose model receives, so the selection and the
// narration can never disagree about which numbers exist.
func (p *Processor) selectSignal(ctx context.Context, insightID string, facts json.RawMessage) (signalSelection, bool, error) {
	if p.verifier == nil {
		return signalSelection{}, false, nil
	}
	var decoded any
	if err := json.Unmarshal(facts, &decoded); err != nil {
		return signalSelection{}, false, err
	}
	questions := map[string]judgment.Question{
		"primary_signal_family":             {Type: "choice", Instructions: "Choose the single most notable thing in these deterministic aggregates, or NONE when they are ordinary. Use OTHER_OR_UNCLEAR when the facts are insufficient.", Criteria: judgment.ChoiceCriteria(signalFamilyCriteria)},
		"spending_change_material":          {Type: "noul", Instructions: "Did spending change materially versus the comparison period?"},
		"category_shift_material":           {Type: "noul", Instructions: "Did the category mix change materially?"},
		"merchant_concentration_noteworthy": {Type: "noul", Instructions: "Is spending concentrated in one merchant enough to be worth telling the household?"},
		"cashflow_pattern_noteworthy":       {Type: "noul", Instructions: "Is the income-versus-outflow pattern worth narrating?"},
		"savings_pattern_noteworthy":        {Type: "noul", Instructions: "Is the savings allocation or unallocated surplus worth narrating?"},
		"material_ambiguity":                {Type: "noul", Instructions: "Are these aggregates genuinely ambiguous or internally inconsistent?"},
	}
	result, err := p.verifier.Evaluate(ctx, insightID+"-signal", judgment.Request{State: map[string]any{"aggregate_facts": decoded}, Questions: questions})
	if err != nil {
		return signalSelection{}, false, err
	}
	selection := signalSelection{Model: result.Model, PolicyVersion: InsightSignalPolicyVersion}
	if answer, ok := result.Answers["primary_signal_family"]; ok {
		criteria := judgment.ChoiceCriteria(signalFamilyCriteria)
		if judgment.AcceptChoice(answer, criteria, signalPolicy.Family) && containsString(signalFamilies, answer.Choice) {
			selection.Family = answer.Choice
		}
	}
	selection.SpendingChange = noulSelected(result.Answers, "spending_change_material")
	selection.CategoryShift = noulSelected(result.Answers, "category_shift_material")
	selection.MerchantConcentration = noulSelected(result.Answers, "merchant_concentration_noteworthy")
	selection.CashflowPattern = noulSelected(result.Answers, "cashflow_pattern_noteworthy")
	selection.SavingsPattern = noulSelected(result.Answers, "savings_pattern_noteworthy")
	selection.MaterialAmbiguity = noulSelected(result.Answers, "material_ambiguity")
	return selection, true, nil
}

// noSignalResponse is the deterministic answer used when the bounded plane finds
// nothing noteworthy. It reports the real reason instead of implying a failure.
const noSignalResponse = "Data keuangan periode ini sudah tercatat dan tidak ada perubahan yang mencolok untuk dibahas. Arus kas, pengeluaran, dan alokasi tabungan masih dalam pola yang biasa."

func noulSelected(answers map[string]judgment.Answer, key string) bool {
	answer, ok := answers[key]
	if !ok {
		return false
	}
	selected, decided := judgment.AcceptNoul(answer, signalPolicy.Signal)
	return selected && decided
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
