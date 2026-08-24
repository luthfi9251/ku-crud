package hooks

import (
	"context"
	"encoding/json"

	h "ku-crud/internal/hooks"
)

func AliasedHook(ctx context.Context, hc *h.HookContext, ev h.Event,
	row h.RowPayload, cfg json.RawMessage) (h.RowPayload, error) {
	return row, nil
}
