package document

import (
	"context"
	"encoding/json"
	"fmt"
)

// shadowObservation is the redacted, shadow-mode comparison record. It stores
// only the model classification signal and the document status at the time of
// shadow observation. No prompt, image, caption, tool arguments, or financial
// value is persisted here.
type shadowObservation struct {
	DocumentType string  `json:"document_type"`
	Tool         string  `json:"tool"`
	Confidence   float64 `json:"confidence"`
}

// recordShadowInterpretation persists the shadow comparison without any
// canonical mutation. It stores a non-validated document_extraction row under a
// dedicated stage so old and new classification can be compared later.
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
