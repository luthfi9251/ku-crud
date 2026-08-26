package ds

import "github.com/luthfi9251/kucrud-core/defs"

type DriftReport struct {
	Missing     []string `json:"missing"`     // defined but dropped from the live table
	Added       []string `json:"added"`       // live but not defined
	TypeChanged []string `json:"typeChanged"` // same name, different field type
}

func (r DriftReport) Empty() bool {
	return len(r.Missing) == 0 && len(r.Added) == 0 && len(r.TypeChanged) == 0
}

// EffectiveType is what drift comparison uses: an fk column tracks its
// underlying introspected type in BaseType.
func EffectiveType(c defs.Column) string {
	if c.FieldType == "fk" && c.BaseType != "" {
		return c.BaseType
	}
	return c.FieldType
}

func CompareDrift(defined []defs.Column, live []LiveColumn) DriftReport {
	var rep DriftReport
	liveByName := map[string]LiveColumn{}
	for _, c := range live {
		liveByName[c.Name] = c
	}
	defNames := map[string]bool{}
	for _, d := range defined {
		defNames[d.Name] = true
		if d.FieldType == "m2m" || d.IsComputed {
			continue // virtual column — nothing to compare against the live schema
		}
		lc, ok := liveByName[d.Name]
		if !ok {
			rep.Missing = append(rep.Missing, d.Name)
			continue
		}
		if lc.FieldType != EffectiveType(d) {
			rep.TypeChanged = append(rep.TypeChanged, d.Name)
		}
	}
	for name := range liveByName {
		if !defNames[name] {
			rep.Added = append(rep.Added, name)
		}
	}
	return rep
}
