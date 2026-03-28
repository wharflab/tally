package php

import "testing"

func TestStageLooksLikeDev(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stage string
		want  bool
	}{
		{name: "plain dev", stage: "dev", want: true},
		{name: "hyphenated token", stage: "builder-dev", want: true},
		{name: "underscored token", stage: "php_test", want: true},
		{name: "debug token", stage: "runtime.debug", want: true},
		{name: "non-dev word", stage: "device", want: false},
		{name: "empty", stage: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stageLooksLikeDev(tt.stage); got != tt.want {
				t.Errorf("stageLooksLikeDev(%q) = %v, want %v", tt.stage, got, tt.want)
			}
		})
	}
}

func TestComposerTruthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "zero", value: "0", want: false},
		{name: "false", value: "false", want: false},
		{name: "empty", value: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := composerTruthy(tt.value); got != tt.want {
				t.Errorf("composerTruthy(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
