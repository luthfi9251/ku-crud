package hooks

import (
	"context"
	"encoding/json"

	kuhooks "github.com/luthfi9251/kucrud-core/hooks"
)

func WrongParams(a int) error { return nil }

func WrongReturn(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
	row kuhooks.RowPayload, cfg json.RawMessage) error {
	return nil
}

var NotAFunc = 1
