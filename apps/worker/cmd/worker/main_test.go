package main

import "testing"

// Production must not be able to disable Jev-owned mutation semantics by
// leaving the model unset; only an explicit non-production opt-out is allowed
// (PRD §28 Configuration).
func TestRequireJudgmentModel(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		model   string
		wantErr bool
	}{
		{name: "unset mode without model is rejected", mode: "", model: "", wantErr: true},
		{name: "whitespace model is rejected", mode: "", model: "   ", wantErr: true},
		{name: "explicit opt-out is allowed", mode: "disabled-dev", model: "", wantErr: false},
		{name: "configured model is allowed", mode: "", model: "jev-latest", wantErr: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := requireJudgmentModel(testCase.mode, testCase.model)
			if testCase.wantErr && err == nil {
				t.Fatal("expected a configuration error")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("unexpected configuration error: %v", err)
			}
		})
	}
}
