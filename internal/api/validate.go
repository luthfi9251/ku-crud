package api

import (
	"fmt"
	"strconv"
	"time"
)

var datetimeLayouts = []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"}

func validateValue(ft string, v any, enum []string) error {
	if v == nil {
		return nil // NULL allowed; required-ness checked separately
	}
	switch ft {
	case "number":
		switch v.(type) {
		case float64, int, int64:
			return nil
		}
		return fmt.Errorf("expected number, got %T", v)
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", v)
		}
	case "text":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected text, got %T", v)
		}
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected datetime string, got %T", v)
		}
		for _, l := range datetimeLayouts {
			if _, err := time.Parse(l, s); err == nil {
				return nil
			}
		}
		return fmt.Errorf("datetime %q not ISO-8601-like", s)
	case "enum":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected enum string, got %T", v)
		}
		for _, o := range enum {
			if o == s {
				return nil
			}
		}
		return fmt.Errorf("%q not in enum options", s)
	default:
		return fmt.Errorf("unknown field type %q", ft)
	}
	return nil
}

func coercePK(ft, raw string) (any, error) {
	if ft == "number" {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i, nil
		}
		return strconv.ParseFloat(raw, 64)
	}
	return raw, nil
}
