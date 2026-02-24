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
