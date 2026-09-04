package telegram

import (
	"encoding/json"
	"fmt"
)

type ProcessPayload struct {
	SourceEventID string `json:"source_event_id"`
}

type CallbackPayload struct {
	SourceEventID   string `json:"source_event_id"`
	CallbackQueryID string `json:"callback_id"`
}

func DecodeCallbackPayload(raw json.RawMessage) (CallbackPayload, error) {
	var payload CallbackPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.SourceEventID == "" || payload.CallbackQueryID == "" {
		return CallbackPayload{}, fmt.Errorf("invalid callback payload")
	}
	return payload, nil
}

func DecodeProcessPayload(raw json.RawMessage) (ProcessPayload, error) {
	var payload ProcessPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ProcessPayload{}, fmt.Errorf("decode process payload: %w", err)
	}
	if payload.SourceEventID == "" {
		return ProcessPayload{}, fmt.Errorf("source event ID is required")
	}
	return payload, nil
}
