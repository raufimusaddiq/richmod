package telegram

import (
	"context"
	"fmt"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

type boundReviewGateway struct{}

func (boundReviewGateway) NativeToolCall(context.Context, string, string, any, []gateway.ToolDefinition, ...gateway.NativeToolOptions) (gateway.ToolCall, gateway.Metadata, error) {
	return gateway.ToolCall{}, gateway.Metadata{}, fmt.Errorf("bound review reply must not call LLM")
}
