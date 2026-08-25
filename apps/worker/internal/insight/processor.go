package insight

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

const prompt = `Write a concise Indonesian household-finance narrative using only the supplied deterministic facts.
Do not recalculate or invent amounts. Do not give investment, tax, credit, or legal advice. Mention uncertainty when completeness is below 0.90.
Return observations, not commands. The household ledger is IDR and the reporting timezone is Asia/Jakarta.`

type Gateway interface {
	Structured(context.Context, string, string, string, any, map[string]any, any) (gateway.Metadata, error)
}

type Processor struct {
	pool    *pgxpool.Pool
	gateway Gateway
}

func NewProcessor(pool *pgxpool.Pool, llm Gateway) *Processor {
	return &Processor{pool: pool, gateway: llm}
}

type Payload struct {
	InsightID string `json:"insight_id"`
}

func DecodePayload(raw json.RawMessage) (Payload, error) {
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.InsightID == "" {
		return Payload{}, fmt.Errorf("invalid insight job payload")
	}
	return payload, nil
}

type output struct {
	Summary            string        `json:"summary"`
	Observations       []observation `json:"observations"`
	DataQualityWarning string        `json:"data_quality_warning"`
	Confidence         float64       `json:"confidence"`
}

type observation struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (p *Processor) Process(ctx context.Context, insightID string) error {
	var householdID, status, completeness string
	var facts json.RawMessage
	if err := p.pool.QueryRow(ctx, `SELECT household_id,status,input_metrics_json,data_completeness::text FROM insight WHERE id=$1`, insightID).Scan(&householdID, &status, &facts, &completeness); err != nil {
		return err
	}
	if status != "PENDING" {
		return nil
	}
	if belowThreshold(completeness, "0.7000") {
		return p.complete(ctx, insightID, householdID, "DETERMINISTIC", "", "Data belum cukup lengkap untuk membuat insight yang andal. Selesaikan Review Inbox dan kategorikan pengeluaran terlebih dahulu.", 1)
	}
	var result output
	metadata, err := p.gateway.Structured(ctx, insightID, "finance.insight", prompt, facts, insightSchema(), &result)
	if err != nil {
		return p.fail(ctx, insightID, householdID)
	}
	text, confidence, err := validateOutput(result)
	if err != nil {
		return p.fail(ctx, insightID, householdID)
	}
	return p.complete(ctx, insightID, householdID, "cloud-llm-gateway", metadata.Model, text, confidence)
}

func belowThreshold(value, threshold string) bool {
	left, ok := new(big.Rat).SetString(value)
	if !ok {
		return true
	}
	right, _ := new(big.Rat).SetString(threshold)
	return left.Cmp(right) < 0
}

func validateOutput(value output) (string, float64, error) {
	value.Summary = strings.TrimSpace(value.Summary)
	value.DataQualityWarning = strings.TrimSpace(value.DataQualityWarning)
	if value.Summary == "" || len([]rune(value.Summary)) > 800 || value.Confidence < 0 || value.Confidence > 1 || len(value.Observations) > 4 || len([]rune(value.DataQualityWarning)) > 400 {
		return "", 0, fmt.Errorf("invalid insight output")
	}
	parts := []string{value.Summary}
	for _, item := range value.Observations {
		title, detail := strings.TrimSpace(item.Title), strings.TrimSpace(item.Detail)
		if title == "" || detail == "" || len([]rune(title)) > 120 || len([]rune(detail)) > 500 {
			return "", 0, fmt.Errorf("invalid insight observation")
		}
		parts = append(parts, "• "+title+": "+detail)
	}
	if value.DataQualityWarning != "" {
		parts = append(parts, "Catatan data: "+value.DataQualityWarning)
	}
	return strings.Join(parts, "\n\n"), value.Confidence, nil
}

func (p *Processor) complete(ctx context.Context, insightID, householdID, route, model, text string, confidence float64) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE insight SET status='SUCCEEDED',gateway_route=$2,model=NULLIF($3,''),generated_text=$4,confidence=$5,completed_at=now() WHERE id=$1 AND status='PENDING'; INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($6,'WORKER','COMPLETE_INSIGHT','insight',$1,jsonb_build_object('gateway_route',$2::text,'model',NULLIF($3,''),'confidence',$5::numeric))`, insightID, route, model, text, confidence, householdID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Processor) fail(ctx context.Context, insightID, householdID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE insight SET status='FAILED',gateway_route='cloud-llm-gateway' WHERE id=$1 AND status='PENDING'; INSERT INTO audit_log(household_id,actor_type,action,entity_type,entity_id,after_json) VALUES($2,'WORKER','FAIL_INSIGHT','insight',$1,jsonb_build_object('reason','gateway_or_validation_failure'))`, insightID, householdID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insightSchema() map[string]any {
	observation := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"title": map[string]any{"type": "string"}, "detail": map[string]any{"type": "string"}}, "required": []string{"title", "detail"}}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"summary": map[string]any{"type": "string"}, "observations": map[string]any{"type": "array", "maxItems": 4, "items": observation}, "data_quality_warning": map[string]any{"type": "string"}, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, "required": []string{"summary", "observations", "data_quality_warning", "confidence"}}
}
