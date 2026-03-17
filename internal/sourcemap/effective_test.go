package sourcemap

import "testing"

func TestEffectiveStartLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		startLine    int
		prevComments []string
		want         int
	}{
		{
			name:         "no comments",
			source:       "FROM alpine\nRUN echo hello\n",
			startLine:    2,
			prevComments: nil,
			want:         2,
		},
		{
			name:   "comment directly adjacent - no leading whitespace",
			source: "FROM alpine\n# comment\nRUN echo hello\n",
			// BuildKit: "# comment"[1:] = " comment" → TrimSpace = "comment"
			startLine:    3,
			prevComments: []string{"comment"},
			want:         2,
		},
		{
			name:   "comment separated by blank line - no leading whitespace",
			source: "FROM alpine\n\n# comment\n\nRUN echo hello\n",
			// BuildKit: "# comment"[1:] = " comment" → TrimSpace = "comment"
			startLine:    5,
			prevComments: []string{"comment"},
			want:         3,
		},
		{
			name:   "indented comment separated by blank line",
			source: "RUN echo hello\n\n    # Haskell dependencies\n\nARG GHC_WASM_META_COMMIT\n",
			// BuildKit: "    # Haskell dependencies"[1:] = "   # Haskell dependencies" → TrimSpace = "# Haskell dependencies"
			startLine:    5,
			prevComments: []string{"# Haskell dependencies"},
			want:         3,
		},
		{
			name:   "multiple comments with blank line between them",
			source: "FROM alpine\n\n# first\n\n# second\nRUN echo hello\n",
			// BuildKit: "# first"[1:] → "first"; "# second"[1:] → "second"
			startLine:    6,
			prevComments: []string{"first", "second"},
			want:         3,
		},
		{
			name:   "multiple adjacent comments",
			source: "FROM alpine\n# first\n# second\nRUN echo hello\n",
			// BuildKit: "# first"[1:] → "first"; "# second"[1:] → "second"
			startLine:    4,
			prevComments: []string{"first", "second"},
			want:         2,
		},
		// Bare "#" edge cases: BuildKit resets its comment accumulator to nil
		// when it encounters a bare "#" (empty comment), so bare "#" is a
		// block-breaker — PrevComment never contains entries from above it.
		{
			name:   "bare hash above comment resets block",
			source: "FROM alpine\n#\n# comment\nRUN echo hello\n",
			// BuildKit: "#"[1:] → "" → resets; "# comment"[1:] → "comment"
			// PrevComment = ["comment"] (only the line after the reset)
			startLine:    4,
			prevComments: []string{"comment"},
			want:         3,
		},
		{
			name:   "bare hash below comment resets block",
			source: "FROM alpine\n# comment\n#\nRUN echo hello\n",
			// BuildKit: "# comment" → ["comment"]; "#" → nil (reset!)
			// PrevComment = [] (empty because bare # reset it)
			startLine:    4,
			prevComments: nil,
			want:         4,
		},
		{
			name:   "bare hash between comments resets block",
			source: "FROM alpine\n# first\n#\n# second\nRUN echo hello\n",
			// BuildKit: "# first" → ["first"]; "#" → nil; "# second" → ["second"]
			// PrevComment = ["second"] (only after the reset)
			startLine:    5,
			prevComments: []string{"second"},
			want:         4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := New([]byte(tt.source))
			got := sm.EffectiveStartLine(tt.startLine, tt.prevComments)
			if got != tt.want {
				t.Errorf("EffectiveStartLine(%d, %v) = %d, want %d",
					tt.startLine, tt.prevComments, got, tt.want)
			}
		})
	}
}

func TestHasBlankLineBetween(t *testing.T) {
	t.Parallel()

	// Source (1-based lines):
	// 1: FROM alpine
	// 2: # comment
	// 3: (blank)
	// 4: ENV FOO=bar
	source := "FROM alpine\n# comment\n\nENV FOO=bar\n"
	sm := New([]byte(source))

	tests := []struct {
		name      string
		startLine int
		endLine   int
		want      bool
	}{
		{"blank between comment and instruction", 2, 4, true},
		{"no blank between instruction and comment", 1, 2, false},
		{"adjacent lines", 1, 2, false},
		{"same line", 3, 3, false},
		{"inverted range", 4, 1, false},
		{"out of range high", 1, 100, true},
		{"out of range low", -5, 4, true},
		{"both out of range no lines", 100, 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sm.HasBlankLineBetween(tt.startLine, tt.endLine)
			if got != tt.want {
				t.Errorf("HasBlankLineBetween(%d, %d) = %v, want %v",
					tt.startLine, tt.endLine, got, tt.want)
			}
		})
	}
}
