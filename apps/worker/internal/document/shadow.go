package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type shadowStart struct {
	At    time.Time
	Value Interpretation
	Model string
}

// shadowObservation is the redacted, shadow-mode comparison record. It stores
// only the model classification signal; no prompt, image, caption, tool
// arguments, or financial value is persisted here.
type shadowObservation struct {
	DocumentType string  `json:"document_type"`
	Tool         string  `json:"tool"`
	Confidence   float64 `json:"confidence"`
}

// shadowComparison is the redacted comparison row. It stores only classification
// outcomes, bounded counters, an error class, and latency. Prompts, evidence,
// captions, filenames, tool arguments, amounts, merchants, and identifiers never
// appear.
type shadowComparison struct {
	ShadowType   string   `json:"shadow_type"`
	ShadowStatus string   `json:"shadow_status"`
	LegacyType   string   `json:"legacy_type"`
	Agreement    string   `json:"agreement"`
	DocumentType string   `json:"document_type"`
	Counters     []string `json:"counters"`
	ErrorClass   string   `json:"error_class"`
	LatencyMS    int64    `json:"latency_ms"`
}

// shadowAgreement classifies one shadow/legacy pair into bounded counters.
// Only classification outcomes are compared; no field values are consulted.
func shadowAgreement(shadow Interpretation, legacy documentClassification) (string, []string) {
	byType := "shadow_by_document_type." + legacy.DocumentType
	if shadow.DocumentType != legacy.DocumentType {
		return "disagree", []string{"disagree", byType}
	}
	if shadowStatus(shadow) != "present" {
		return "malformed", []string{"malformed", byType}
	}
	return "agree", []string{"agree", byType}
}

// recordShadowInterpretation persists the shadow comparison without any
// canonical mutation.
func (p *Processor) recordShadowInterpretation(ctx context.Context, householdID, sourceID, documentID string, value Interpretation, model string) error {
	payload, err := json.Marshal(shadowObservation{DocumentType: value.DocumentType, Tool: value.Tool, Confidence: value.Confidence})
	if err != nil {
		return fmt.Errorf("encode shadow interpretation: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO document_extraction(document_id,stage,schema_version,output_json,confidence,gateway_model,validated) VALUES($1,'INTERPRETATION_SHADOW','1',$2::jsonb,$3,$4,false) ON CONFLICT(document_id,stage,schema_version) DO NOTHING`, documentID, string(payload), value.Confidence, model); err != nil {
		return fmt.Errorf("record shadow interpretation: %w", err)
	}
	return nil
}

// recordShadowComparison stores only agreement, bounded counters, and latency.
func (p *Processor) recordShadowComparison(ctx context.Context, documentID string, shadow Interpretation, shadowModel string, legacy documentClassification, elapsed time.Duration) error {
	agreement, counters := shadowAgreement(shadow, legacy)
	payload, err := json.Marshal(shadowComparison{
		ShadowType:   shadow.DocumentType,
		ShadowStatus: shadowStatus(shadow),
		LegacyType:   legacy.DocumentType,
		Agreement:    agreement,
		DocumentType: legacy.DocumentType,
		Counters:     counters,
		LatencyMS:    elapsed.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("encode shadow comparison: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO document_extraction(document_id,stage,schema_version,output_json,confidence,gateway_model,validated) VALUES($1,'INTERPRETATION_SHADOW_METRIC','1',$2::jsonb,$3,$4,false) ON CONFLICT(document_id,stage,schema_version) DO NOTHING`, documentID, string(payload), shadow.Confidence, shadowModel); err != nil {
		return fmt.Errorf("record shadow comparison: %w", err)
	}
	return nil
}

// shadowStatus maps a shadow observation to a bounded status token.
func shadowStatus(shadow Interpretation) string {
	if shadow.DocumentType == "" || shadow.Confidence < 0 || shadow.Confidence > 1 || (shadow.Quality != "" && shadow.Quality != QualityClear && shadow.Quality != QualityDegraded && shadow.Quality != QualityUnreadable) {
		return "malformed"
	}
	if len(shadow.AmbiguousFields) > 0 {
		return "ambiguous"
	}
	if len(shadow.MissingFields) > 0 {
		return "missing"
	}
	for _, field := range shadow.Fields {
		switch field.Status {
		case ObservationAmbiguous:
			return "ambiguous"
		case ObservationMissing:
			return "missing"
		}
	}
	return "present"
}

// recordShadowFailure stores only a bounded error class and latency.
func (p *Processor) recordShadowFailure(ctx context.Context, documentID, errorType string, elapsed time.Duration) error {
	class := shadowErrorClass(errorType)
	payload, err := json.Marshal(shadowComparison{
		Agreement:  "error",
		Counters:   []string{"error", "error_class." + class},
		ErrorClass: class,
		LatencyMS:  elapsed.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("encode shadow failure: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO document_extraction(document_id,stage,schema_version,output_json,confidence,gateway_model,validated) VALUES($1,'INTERPRETATION_SHADOW_METRIC','1',$2::jsonb,NULL,'',false) ON CONFLICT(document_id,stage,schema_version) DO NOTHING`, documentID, string(payload)); err != nil {
		return fmt.Errorf("record shadow failure: %w", err)
	}
	return nil
}

// shadowErrorClass reduces an error to one of a fixed, redacted set.
func shadowErrorClass(errorType string) string {
	switch {
	case strings.Contains(errorType, "ToolCall"):
		return "gateway_tool_call"
	case strings.Contains(errorType, "Decode"), strings.Contains(errorType, "Unmarshal"):
		return "malformed_payload"
	default:
		return "unknown_error"
	}
}
