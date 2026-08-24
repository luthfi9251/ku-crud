package hooks

import (
	"context"
	"encoding/json"
	"math"

	kuhooks "ku-crud/internal/hooks"
)

// NormalizePrice is an example before-create hook: rounds the "price"
// column to the number of decimals given in the assignment config
// ({"decimals": 2}). Copy this file's shape to write real hooks.
func NormalizePrice(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
	row kuhooks.RowPayload, cfg json.RawMessage) (kuhooks.RowPayload, error) {
	v, ok := row.Values["price"]
	if !ok || v == nil {
		return row, nil
	}
	f, ok := v.(float64)
	if !ok {
		return row, nil
	}
	dec := 2
	var c struct{ Decimals int }
	if len(cfg) > 0 {
		json.Unmarshal(cfg, &c)
		if c.Decimals > 0 && c.Decimals <= 6 {
			dec = c.Decimals
		}
	}
	shift := math.Pow(10, float64(dec))
	row.Values["price"] = math.Round(f*shift) / shift
	return row, nil
}

// LogAfterCreate is an example after-create hook: side effect only.
func LogAfterCreate(ctx context.Context, hc *kuhooks.HookContext, ev kuhooks.Event,
	row kuhooks.RowPayload, cfg json.RawMessage) (kuhooks.RowPayload, error) {
	hc.Logger.Info("row created", "table", hc.TableDef.Label, "values", row.Values)
	return row, nil
}
