package stagename

import "testing"

func TestLooksLikeDev(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stage string
		want  bool
	}{
		{name: "plain dev", stage: "dev", want: true},
		{name: "hyphenated token", stage: "builder-dev", want: true},
		{name: "underscored token", stage: "php_test", want: true},
		{name: "dotted debug token", stage: "runtime.debug", want: true},
		{name: "slashed ci token", stage: "build/ci", want: true},
		{name: "colon-separated testing token", stage: "stage:testing", want: true},
		{name: "uppercase Development", stage: "Development", want: true},
		{name: "leading whitespace", stage: "   dev  ", want: true},
		{name: "non-dev word", stage: "device", want: false},
		{name: "production-deploy", stage: "production-deploy", want: false},
		{name: "empty", stage: "", want: false},
		{name: "delimiters only", stage: "---", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := LooksLikeDev(tt.stage); got != tt.want {
				t.Errorf("LooksLikeDev(%q) = %v, want %v", tt.stage, got, tt.want)
			}
		})
	}
}
