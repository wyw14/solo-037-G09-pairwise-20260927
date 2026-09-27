package model

import (
    "strconv"
    "strings"
)

// ConditionField names the physical conditions that must agree for two tows
// to be treated as repetitions. Keep the order stable: it drives report rows.
const (
    FieldModelName     = "model_name"
    FieldSection       = "section"
    FieldCarriage      = "carriage"
    FieldTowSpeed      = "tow_speed"
    FieldWaveCondition = "wave_condition"
    FieldLoading       = "loading"
)

// ConditionFields lists every compared field in display order.
func ConditionFields() []string {
    return []string{
        FieldModelName,
        FieldSection,
        FieldCarriage,
        FieldTowSpeed,
        FieldWaveCondition,
        FieldLoading,
    }
}

// Conditions describes the physical set-up of a single tow. The zero value
// means "not configured" (which is what an operator-measurements.json written
// before this feature deserializes to); it must never be treated as a shared
// condition, so callers check Configured before Equal.
type Conditions struct {
    TowSpeed      float64 `json:"tow_speed"`      // m/s, must be > 0 when configured
    WaveCondition string  `json:"wave_condition"` // e.g. "calm" or "regular:H=0.1,T=1.5"
    Loading       string  `json:"loading"`        // loading condition / draught, free text
}

// Normalized returns a copy with text fields trimmed.
func (c Conditions) Normalized() Conditions {
    return Conditions{
        TowSpeed:      c.TowSpeed,
        WaveCondition: strings.TrimSpace(c.WaveCondition),
        Loading:       strings.TrimSpace(c.Loading),
    }
}

// Configured reports whether every condition was supplied. Conditions are
// optional at run creation but they must be supplied as a complete set.
func (c Conditions) Configured() bool {
    c = c.Normalized()
    return c.TowSpeed > 0 && c.WaveCondition != "" && c.Loading != ""
}

// PartiallyConfigured reports that some but not all conditions were given.
func (c Conditions) PartiallyConfigured() bool {
    c = c.Normalized()
    given := c.TowSpeed > 0 || c.WaveCondition != "" || c.Loading != ""
    return given && !c.Configured()
}

// Equal compares conditions exactly. Tow speed comes from the same JSON entry
// text and survives an identical round trip, so bit-wise equality is used on
// purpose: a tolerance would silently hide entry mistakes that the operator is
// meant to see as a condition difference. Callers should have verified both
// sides are Configured.
func (c Conditions) Equal(other Conditions) bool {
    c, other = c.Normalized(), other.Normalized()
    return c.TowSpeed == other.TowSpeed &&
        c.WaveCondition == other.WaveCondition &&
        c.Loading == other.Loading
}

// RenderField renders one condition field for display. The boolean is false
// when the value is absent/unconfigured, which the report renders as null.
func (c Conditions) RenderField(field string) (string, bool) {
    c = c.Normalized()
    switch field {
    case FieldTowSpeed:
        if c.TowSpeed <= 0 {
            return "", false
        }
        return strconv.FormatFloat(c.TowSpeed, 'g', -1, 64), true
    case FieldWaveCondition:
        if c.WaveCondition == "" {
            return "", false
        }
        return c.WaveCondition, true
    case FieldLoading:
        if c.Loading == "" {
            return "", false
        }
        return c.Loading, true
    default:
        return "", false
    }
}
