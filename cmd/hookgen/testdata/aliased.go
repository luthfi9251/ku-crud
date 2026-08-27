package hooks

import (
	"context"
	"encoding/json"

	h "github.com/luthfi9251/ku-crud/core/hooks"
)

func AliasedHook(ctx context.Context, hc *h.HookContext, ev h.Event,
	row h.RowPayload, cfg json.RawMessage) (h.RowPayload, error) {
	return row, nil
}
