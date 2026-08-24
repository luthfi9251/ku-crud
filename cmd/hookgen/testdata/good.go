package hooks

import (
	"context"
	"encoding/json"

	kuhooks "ku-crud/internal/hooks"
)

func GoodHook(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
	row kuhooks.RowPayload, cfg json.RawMessage) (kuhooks.RowPayload, error) {
	return row, nil
}
