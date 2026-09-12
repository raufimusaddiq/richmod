package telegram

import "strings"

// stringPtr normalizes optional model arguments without letting empty strings
// masquerade as supplied financial facts.
func stringPtr(value any) *string {
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case *string:
		if typed == nil {
			return nil
		}
		raw = *typed
	default:
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return &raw
}
