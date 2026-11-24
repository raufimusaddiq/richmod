package main

import (
	"testing"

	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func TestPhaseMetricDimensionsAreNotNull(t *testing.T) {
	phase := phaseMetric(gateway.CallMetric{Capability: "GENERATIVE", Dimensions: []string{"category"}}, "")
	if phase.Dimensions == nil || phase.AnsweredDimensions == nil || phase.ResidualDimensions == nil {
		t.Fatal("telemetry dimensions must be non-null PostgreSQL arrays")
	}
	if len(phase.Dimensions) != 1 || phase.Dimensions[0] != "category" {
		t.Fatalf("existing dimensions changed: %v", phase.Dimensions)
	}
}

func TestEnvEnabledDefaultsOnForUnsetOrUnknownValues(t *testing.T) {
	cases := map[string]bool{
		"":           true,
		"1":          true,
		"true":       true,
		"yes":        true,
		"0":          false,
		"false":      false,
		"OFF":        false,
		"no":         false,
		"disabled":   false,
		" disabled ": false,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("RICHMOD_TEST_SWITCH", value)
			if got := envEnabled("RICHMOD_TEST_SWITCH"); got != want {
				t.Fatalf("envEnabled(%q)=%v want %v", value, got, want)
			}
		})
	}
}
