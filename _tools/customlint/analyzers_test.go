package customlint

import (
	"testing"
)

// TestAnalyzerNames verifies that all analyzers have meaningful names.
func TestAnalyzerNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		analyzer string
		wantName string
	}{
		{"ruleStructAnalyzer", ruleStructAnalyzer.Name, "rulestruct"},
		{"lspLiteralAnalyzer", lspLiteralAnalyzer.Name, "lspliteral"},
		{"docURLAnalyzer", docURLAnalyzer.Name, "docurl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.analyzer != tt.wantName {
				t.Errorf("%s.Name = %q, want %q", tt.name, tt.analyzer, tt.wantName)
			}
		})
	}
}

// TestAnalyzerDocs verifies that all analyzers have documentation.
func TestAnalyzerDocs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		analyzer string
	}{
		{"ruleStructAnalyzer", ruleStructAnalyzer.Doc},
		{"lspLiteralAnalyzer", lspLiteralAnalyzer.Doc},
		{"docURLAnalyzer", docURLAnalyzer.Doc},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.analyzer == "" {
				t.Errorf("%s has empty documentation", tt.name)
			}
			if len(tt.analyzer) < 10 {
				t.Errorf("%s documentation is too short: %q", tt.name, tt.analyzer)
			}
		})
	}
}

// TestLSPMethodDetection tests the isLSPMethod helper function directly.
func TestLSPMethodDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		want   bool
		reason string
	}{
		{
			name:   "exact_match_initialize",
			input:  "initialize",
			want:   true,
			reason: "exact match from lspMethodExact map",
		},
		{
			name:   "exact_match_shutdown",
			input:  "shutdown",
			want:   true,
			reason: "exact match from lspMethodExact map",
		},
		{
			name:   "exact_match_exit",
			input:  "exit",
			want:   true,
			reason: "exact match from lspMethodExact map",
		},
		{
			name:   "prefix_textDocument",
			input:  "textDocument/didOpen",
			want:   true,
			reason: "matches textDocument/ prefix",
		},
		{
			name:   "prefix_workspace",
			input:  "workspace/didChangeConfiguration",
			want:   true,
			reason: "matches workspace/ prefix",
		},
		{
			name:   "prefix_dollar",
			input:  "$/cancelRequest",
			want:   true,
			reason: "matches $/ prefix",
		},
		{
			name:   "non_lsp_method",
			input:  "something/else",
			want:   false,
			reason: "doesn't match any LSP prefix or exact name",
		},
		{
			name:   "empty_string",
			input:  "",
			want:   false,
			reason: "empty string is not an LSP method",
		},
		{
			name:   "random_string",
			input:  "hello world",
			want:   false,
			reason: "random string is not an LSP method",
		},
		{
			name:   "partial_match_not_enough",
			input:  "text",
			want:   false,
			reason: "partial match without slash is not valid",
		},
		{
			name:   "case_sensitive",
			input:  "Initialize",
			want:   false,
			reason: "LSP method names are case-sensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLSPMethod(tt.input)
			if got != tt.want {
				t.Errorf("isLSPMethod(%q) = %v, want %v (reason: %s)", tt.input, got, tt.want, tt.reason)
			}
		})
	}
}

// TestLSPMethodPrefixes verifies that the prefix list is not empty.
func TestLSPMethodPrefixes(t *testing.T) {
	t.Parallel()

	if len(lspMethodPrefixes) == 0 {
		t.Error("lspMethodPrefixes should not be empty")
	}

	// Verify all prefixes end with either "/" or are valid patterns
	for i, prefix := range lspMethodPrefixes {
		if prefix == "" {
			t.Errorf("lspMethodPrefixes[%d] is empty", i)
		}
	}
}

// TestLSPMethodExact verifies that the exact match map is not empty.
func TestLSPMethodExact(t *testing.T) {
	t.Parallel()

	if len(lspMethodExact) == 0 {
		t.Error("lspMethodExact should not be empty")
	}

	// Verify all keys are non-empty
	for key := range lspMethodExact {
		if key == "" {
			t.Error("lspMethodExact contains empty key")
		}
	}
}