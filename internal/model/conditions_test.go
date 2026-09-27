package model

import (
    "encoding/json"
    "testing"
    "time"
)

func TestConditionsZeroValueIsUnconfigured(t *testing.T) {
    var c Conditions
    if c.Configured() {
        t.Fatal("zero conditions must not be configured (legacy archives)")
    }
    // Note: Equal(zero, zero) is technically true, which is why every caller
    // must gate comparisons on Configured first (exercised in service tests).
    if c.PartiallyConfigured() {
        t.Fatal("blank conditions must not look partially configured")
    }
}

func TestConditionsConfigurationRules(t *testing.T) {
    base := Conditions{TowSpeed: 1.2, WaveCondition: "calm", Loading: "draft-0.3"}
    if !base.Configured() {
        t.Fatal("complete conditions rejected")
    }
    partial := []Conditions{
        {TowSpeed: 1.2},
        {WaveCondition: "calm"},
        {Loading: "draft-0.3"},
        {TowSpeed: 1.2, WaveCondition: "calm"},
        {TowSpeed: 0, WaveCondition: "calm", Loading: "draft-0.3"},
        {TowSpeed: -1, WaveCondition: "calm", Loading: "draft-0.3"},
        {TowSpeed: 1.2, WaveCondition: "  ", Loading: "draft-0.3"},
    }
    for index, c := range partial {
        if c.Configured() || !c.PartiallyConfigured() {
            t.Fatalf("partial case %d misclassified: configured=%v partial=%v", index, c.Configured(), c.PartiallyConfigured())
        }
    }
}

func TestConditionsEqualNormalizesAndIsExact(t *testing.T) {
    a := Conditions{TowSpeed: 1.5, WaveCondition: "calm", Loading: "draft 0.3"}
    b := Conditions{TowSpeed: 1.5, WaveCondition: "  calm ", Loading: "draft 0.3"}
    if !a.Equal(b) {
        t.Fatal("trimmed text should compare equal")
    }
    if a.Equal(Conditions{TowSpeed: 1.6, WaveCondition: "calm", Loading: "draft 0.3"}) {
        t.Fatal("tow speed difference must compare unequal")
    }
    if a.Equal(Conditions{TowSpeed: 1.5, WaveCondition: "regular", Loading: "draft 0.3"}) {
        t.Fatal("wave condition difference must compare unequal")
    }
}

func TestConditionsRenderField(t *testing.T) {
    c := Conditions{TowSpeed: 1.5, WaveCondition: "calm", Loading: "draft 0.3"}
    cases := map[string]string{
        FieldTowSpeed:      "1.5",
        FieldWaveCondition: "calm",
        FieldLoading:       "draft 0.3",
    }
    for field, want := range cases {
        got, ok := c.RenderField(field)
        if !ok || got != want {
            t.Fatalf("field %s rendered %q,%v want %q", field, got, ok, want)
        }
    }
    var blank Conditions
    for _, field := range []string{FieldTowSpeed, FieldWaveCondition, FieldLoading} {
        if _, ok := blank.RenderField(field); ok {
            t.Fatalf("blank %s must render as absent", field)
        }
    }
    if _, ok := c.RenderField(FieldModelName); ok {
        t.Fatal("non-condition field must not render")
    }
}

func TestOperationConditionsRoundTrip(t *testing.T) {
    op := NewOperation("tow", "hull", "section-a", "carriage-1", time.Now().UTC()).
        WithConditions(Conditions{TowSpeed: 1.25, WaveCondition: "calm", Loading: "draft"})
    data, err := json.Marshal(op)
    if err != nil {
        t.Fatal(err)
    }
    var decoded Operation
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatal(err)
    }
    if !decoded.Conditions.Equal(op.Conditions) || !decoded.Conditions.Configured() {
        t.Fatalf("conditions did not survive JSON round trip: %+v", decoded.Conditions)
    }
}

func TestLegacyOperationWithoutConditionsKey(t *testing.T) {
    legacy := `{"id":"00000000-0000-0000-0000-000000000001","name":"tow","model_name":"hull","section":"section-a","carriage":"carriage-1","stage":"configured","revision":"00000000-0000-0000-0000-000000000002","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
    var op Operation
    if err := json.Unmarshal([]byte(legacy), &op); err != nil {
        t.Fatal(err)
    }
    if op.Conditions.Configured() {
        t.Fatal("legacy operation without conditions must deserialize unconfigured")
    }
}
