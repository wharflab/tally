package fixes

import (
	"bytes"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

func TestFromAsCasingFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    string
		wantFix   bool
		wantNewAS string
		wantEdits int
	}{
		{
			name:      "uppercase FROM with lowercase as",
			source:    "FROM alpine as builder",
			wantFix:   true,
			wantNewAS: "AS",
			wantEdits: 1,
		},
		{
			name:      "lowercase from with uppercase AS",
			source:    "from alpine AS builder",
			wantFix:   true,
			wantNewAS: "as",
			wantEdits: 1,
		},
		{
			name:    "matching uppercase",
			source:  "FROM alpine AS builder",
			wantFix: false,
		},
		{
			name:    "matching lowercase",
			source:  "from alpine as builder",
			wantFix: false,
		},
		{
			name:    "no AS clause",
			source:  "FROM alpine:3.18",
			wantFix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
				RuleCode: rules.BuildKitRulePrefix + "FromAsCasing",
				Message:  "'as' and 'FROM' keywords' casing do not match",
			}

			enrichFromAsCasingFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, tt.wantEdits)
				assert.Equal(t, tt.wantNewAS, v.SuggestedFix.Edits[0].NewText)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestStageNameCasingFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		source        string
		stageName     string
		wantFix       bool
		wantLowerName string
		wantEditCount int
	}{
		{
			name:          "simple uppercase stage name",
			source:        "FROM alpine AS Builder\nRUN echo hello",
			stageName:     "Builder",
			wantFix:       true,
			wantLowerName: "builder",
			wantEditCount: 1,
		},
		{
			name:          "stage name with COPY --from reference",
			source:        "FROM alpine AS Builder\nRUN echo hello\nFROM alpine\nCOPY --from=Builder /app /app",
			stageName:     "Builder",
			wantFix:       true,
			wantLowerName: "builder",
			wantEditCount: 2, // Stage def + COPY --from
		},
		{
			name:          "stage name with FROM reference",
			source:        "FROM alpine AS Builder\nRUN echo hello\nFROM Builder",
			stageName:     "Builder",
			wantFix:       true,
			wantLowerName: "builder",
			wantEditCount: 2, // Stage def + FROM
		},
		{
			name:          "mixed case with multiple references",
			source:        "FROM alpine AS MyStage\nRUN echo hello\nFROM alpine\nCOPY --from=MyStage /a /a\nFROM MyStage AS final",
			stageName:     "MyStage",
			wantFix:       true,
			wantLowerName: "mystage",
			wantEditCount: 3, // Stage def + COPY --from + FROM
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)

			// Parse the Dockerfile to get semantic model
			parseResult, err := dockerfile.Parse(bytes.NewReader(source), nil)
			require.NoError(t, err)

			sem := semantic.NewBuilder(parseResult, nil, "test.Dockerfile").Build()

			// Create violation mimicking BuildKit's message
			firstRange := parser.Range{
				Start: parser.Position{Line: 1, Character: 0},
				End:   parser.Position{Line: 1, Character: len(tt.source)},
			}
			v := rules.Violation{
				Location: rules.NewLocationFromRange("test.Dockerfile", firstRange),
				RuleCode: rules.BuildKitRulePrefix + "StageNameCasing",
				Message:  "Stage name '" + tt.stageName + "' should be lowercase",
			}

			enrichStageNameCasingFix(&v, sem, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.Len(t, v.SuggestedFix.Edits, tt.wantEditCount)

				// All edits should replace with lowercase
				for _, edit := range v.SuggestedFix.Edits {
					assert.Equal(t, tt.wantLowerName, edit.NewText)
				}
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestEnrichBuildKitFixes(t *testing.T) {
	t.Parallel()
	source := []byte("FROM alpine as Builder\nRUN echo hello")

	parseResult, err := dockerfile.Parse(bytes.NewReader(source), nil)
	require.NoError(t, err)

	sem := semantic.NewBuilder(parseResult, nil, "test.Dockerfile").Build()

	// Create violations for both rules
	violations := []rules.Violation{
		{
			Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, 22),
			RuleCode: rules.BuildKitRulePrefix + "FromAsCasing",
			Message:  "'as' and 'FROM' keywords' casing do not match",
		},
		{
			Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, 22),
			RuleCode: rules.BuildKitRulePrefix + "StageNameCasing",
			Message:  "Stage name 'Builder' should be lowercase",
		},
		{
			// Non-BuildKit rule should be skipped
			Location: rules.NewRangeLocation("test.Dockerfile", 2, 0, 2, 14),
			RuleCode: rules.HadolintRulePrefix + "DL3027",
			Message:  "some message",
		},
	}

	EnrichBuildKitFixes(violations, sem, source)

	// FromAsCasing should have a fix
	require.NotNil(t, violations[0].SuggestedFix)
	assert.Equal(t, "AS", violations[0].SuggestedFix.Edits[0].NewText)

	// StageNameCasing should have a fix
	require.NotNil(t, violations[1].SuggestedFix)
	assert.Equal(t, "builder", violations[1].SuggestedFix.Edits[0].NewText)

	// Hadolint rule should NOT have a fix (not processed by enricher)
	assert.Nil(t, violations[2].SuggestedFix)
}

func TestFindASKeyword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		line          string
		wantASStart   int
		wantASEnd     int
		wantNameStart int
		wantNameEnd   int
	}{
		{
			name:          "simple FROM AS",
			line:          "FROM alpine AS builder",
			wantASStart:   12,
			wantASEnd:     14,
			wantNameStart: 15,
			wantNameEnd:   22,
		},
		{
			name:          "lowercase as",
			line:          "FROM alpine as builder",
			wantASStart:   12,
			wantASEnd:     14,
			wantNameStart: 15,
			wantNameEnd:   22,
		},
		{
			name:          "with platform",
			line:          "FROM --platform=linux/amd64 alpine AS builder",
			wantASStart:   35,
			wantASEnd:     37,
			wantNameStart: 38,
			wantNameEnd:   45,
		},
		{
			name:          "no AS keyword",
			line:          "FROM alpine:3.18",
			wantASStart:   -1,
			wantASEnd:     -1,
			wantNameStart: -1,
			wantNameEnd:   -1,
		},
		{
			name:          "stage name with dot",
			line:          "FROM alpine AS builder.v1",
			wantASStart:   12,
			wantASEnd:     14,
			wantNameStart: 15,
			wantNameEnd:   25, // "builder.v1" is 10 chars
		},
		{
			name:          "stage name with underscore and dot",
			line:          "FROM alpine AS my_stage.test",
			wantASStart:   12,
			wantASEnd:     14,
			wantNameStart: 15,
			wantNameEnd:   28, // "my_stage.test" is 13 chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			asStart, asEnd, nameStart, nameEnd := findASKeyword([]byte(tt.line))
			assert.Equal(t, tt.wantASStart, asStart, "asStart")
			assert.Equal(t, tt.wantASEnd, asEnd, "asEnd")
			assert.Equal(t, tt.wantNameStart, nameStart, "nameStart")
			assert.Equal(t, tt.wantNameEnd, nameEnd, "nameEnd")
		})
	}
}

func TestFindCopyFromValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		line      string
		wantStart int
		wantEnd   int
	}{
		{
			name:      "simple COPY --from",
			line:      "COPY --from=builder /app /app",
			wantStart: 12,
			wantEnd:   19,
		},
		{
			name:      "COPY with multiple flags",
			line:      "COPY --chown=user --from=Builder /src /dst",
			wantStart: 25,
			wantEnd:   32,
		},
		{
			name:      "no --from flag",
			line:      "COPY /app /app",
			wantStart: -1,
			wantEnd:   -1,
		},
		{
			name:      "uppercase FROM",
			line:      "COPY --FROM=Builder /app /app",
			wantStart: 12,
			wantEnd:   19,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, end := findCopyFromValue([]byte(tt.line))
			assert.Equal(t, tt.wantStart, start, "start")
			assert.Equal(t, tt.wantEnd, end, "end")
		})
	}
}

func TestNoEmptyContinuationFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		source           string
		violationLine    int // 1-based line from BuildKit
		endLine          int // 0 means same as violationLine
		wantFix          bool
		wantEditCount    int
		wantRemovedLines []int // 1-based line numbers
	}{
		{
			name:             "single empty continuation line",
			source:           "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add curl",
			violationLine:    4,
			wantFix:          true,
			wantEditCount:    1,
			wantRemovedLines: []int{3},
		},
		{
			name:             "multiple empty continuation lines",
			source:           "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add \\\n\n    curl",
			violationLine:    6,
			wantFix:          true,
			wantEditCount:    2,
			wantRemovedLines: []int{3, 5},
		},
		{
			name:          "no empty continuation lines",
			source:        "FROM alpine:3.18\nRUN apk update && \\\n    apk add curl",
			violationLine: 3,
			wantFix:       false,
		},
		{
			name:          "empty line not in continuation",
			source:        "FROM alpine:3.18\n\nRUN echo hello",
			violationLine: 3,
			wantFix:       false,
		},
		{
			name:          "violation line is zero",
			source:        "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add curl",
			violationLine: 0,
			wantFix:       false,
		},
		{
			name:          "violation line exceeds source lines",
			source:        "FROM alpine:3.18\nRUN echo hello",
			violationLine: 10,
			wantFix:       false,
		},
		{
			name:          "end line zero falls back to start line",
			source:        "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add curl",
			violationLine: 4,
			endLine:       0, // Will use Start.Line
			wantFix:       true,
			wantEditCount: 1,
		},
		{
			name:             "CRLF line endings",
			source:           "FROM alpine:3.18\r\nRUN apk update && \\\r\n\r\n    apk add curl",
			violationLine:    4,
			wantFix:          true,
			wantEditCount:    1,
			wantRemovedLines: []int{3},
		},
		{
			// Empty line is the last line of the file (no content after it)
			// The edit spans from end of previous line to start of empty line to remove the newline
			name:             "empty continuation as last line",
			source:           "FROM alpine:3.18\nRUN echo \\\n\n",
			violationLine:    3,
			wantFix:          true,
			wantEditCount:    1,
			wantRemovedLines: []int{2}, // Edit starts at line 2 (prev line end)
		},
		{
			name:          "content line in middle of multiline command",
			source:        "FROM alpine:3.18\nRUN apk update && \\\n    apk add curl && \\\n    echo done",
			violationLine: 4,
			wantFix:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			endLine := tt.endLine
			if endLine == 0 {
				endLine = tt.violationLine
			}
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", tt.violationLine, 0, endLine, 0),
				RuleCode: rules.BuildKitRulePrefix + "NoEmptyContinuation",
				Message:  "Empty continuation line found in: RUN ...",
			}

			enrichNoEmptyContinuationFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, tt.wantEditCount, "edit count mismatch")
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)

				// Verify each edit removes the correct line
				for i, edit := range v.SuggestedFix.Edits {
					assert.Empty(t, edit.NewText, "edit %d should have empty NewText", i)
					if i < len(tt.wantRemovedLines) {
						assert.Equal(t, tt.wantRemovedLines[i], edit.Location.Start.Line, "edit %d wrong line", i)
					}
				}
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    string
		wantLines []string
	}{
		{
			name:      "LF line endings",
			source:    "line1\nline2\nline3",
			wantLines: []string{"line1", "line2", "line3"},
		},
		{
			name:      "CRLF line endings",
			source:    "line1\r\nline2\r\nline3",
			wantLines: []string{"line1", "line2", "line3"},
		},
		{
			name:      "mixed line endings",
			source:    "line1\nline2\r\nline3",
			wantLines: []string{"line1", "line2", "line3"},
		},
		{
			name:      "trailing newline",
			source:    "line1\nline2\n",
			wantLines: []string{"line1", "line2"},
		},
		{
			name:      "empty source",
			source:    "",
			wantLines: []string{},
		},
		{
			name:      "single line no newline",
			source:    "single",
			wantLines: []string{"single"},
		},
		{
			name:      "empty lines",
			source:    "line1\n\nline3",
			wantLines: []string{"line1", "", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitLines([]byte(tt.source))
			gotStrings := make([]string, len(got))
			for i, line := range got {
				gotStrings[i] = string(line)
			}
			assert.Equal(t, tt.wantLines, gotStrings)
		})
	}
}

func TestHasContinuationBefore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		lines    []string
		emptyIdx int
		want     bool
	}{
		{
			name:     "continuation before empty line",
			lines:    []string{"RUN echo \\", "", "done"},
			emptyIdx: 1,
			want:     true,
		},
		{
			name:     "no continuation before empty line",
			lines:    []string{"RUN echo", "", "done"},
			emptyIdx: 1,
			want:     false,
		},
		{
			name:     "multiple empty lines after continuation",
			lines:    []string{"RUN echo \\", "", "", "done"},
			emptyIdx: 2,
			want:     true,
		},
		{
			name:     "empty at start",
			lines:    []string{"", "RUN echo"},
			emptyIdx: 0,
			want:     false,
		},
		{
			name:     "continuation with whitespace",
			lines:    []string{"  RUN echo \\  ", "", "done"},
			emptyIdx: 1,
			want:     true,
		},
		// Additional cases (from isPartOfMultilineCommand consolidation)
		{
			name:     "line after continuation",
			lines:    []string{"RUN echo \\", "done"},
			emptyIdx: 1,
			want:     true,
		},
		{
			name:     "line not after continuation",
			lines:    []string{"RUN echo", "done"},
			emptyIdx: 1,
			want:     false,
		},
		{
			name:     "first line",
			lines:    []string{"RUN echo"},
			emptyIdx: 0,
			want:     false,
		},
		{
			name:     "line after empty line after continuation",
			lines:    []string{"RUN echo \\", "", "done"},
			emptyIdx: 2,
			want:     true,
		},
		{
			name:     "line after multiple empty lines no continuation",
			lines:    []string{"RUN echo", "", "", "done"},
			emptyIdx: 3,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := make([][]byte, len(tt.lines))
			for i, s := range tt.lines {
				lines[i] = []byte(s)
			}
			got := hasContinuationBefore(lines, tt.emptyIdx)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMaintainerDeprecatedFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   string
		wantFix  bool
		wantText string
	}{
		{
			name:     "simple maintainer with email",
			source:   "MAINTAINER john@example.com",
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="john@example.com"`,
		},
		{
			name:     "maintainer with name and email",
			source:   "MAINTAINER John Doe <john@example.com>",
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="John Doe <john@example.com>"`,
		},
		{
			name:     "maintainer with double quotes",
			source:   `MAINTAINER "John Doe <john@example.com>"`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="John Doe <john@example.com>"`,
		},
		{
			name:     "maintainer with single quotes",
			source:   `MAINTAINER 'John Doe <john@example.com>'`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="John Doe <john@example.com>"`,
		},
		{
			name:     "maintainer with extra whitespace",
			source:   "MAINTAINER   john@example.com  ",
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="john@example.com"`,
		},
		{
			name:     "lowercase maintainer",
			source:   "maintainer john@example.com",
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="john@example.com"`,
		},
		{
			name:    "empty maintainer value",
			source:  "MAINTAINER   ",
			wantFix: false,
		},
		{
			name:     "maintainer with embedded quotes",
			source:   `MAINTAINER John "The Dev" Doe <john@example.com>`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="John \"The Dev\" Doe <john@example.com>"`,
		},
		{
			name:     "maintainer with backslash",
			source:   `MAINTAINER John\Doe <john@example.com>`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="John\\Doe <john@example.com>"`,
		},
		{
			name:     "maintainer with both quotes and backslash",
			source:   `MAINTAINER "John \"Dev\" Doe\Developer" <john@example.com>`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="\"John \\\"Dev\\\" Doe\\Developer\" <john@example.com>"`,
		},
		{
			name:     "maintainer with unmatched leading quote",
			source:   `MAINTAINER "John Doe <john@example.com>`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="\"John Doe <john@example.com>"`,
		},
		{
			name:     "maintainer with unmatched trailing quote",
			source:   `MAINTAINER John Doe" <john@example.com>`,
			wantFix:  true,
			wantText: `LABEL org.opencontainers.image.authors="John Doe\" <john@example.com>"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
				RuleCode: rules.BuildKitRulePrefix + "MaintainerDeprecated",
				Message:  "Maintainer instruction is deprecated in favor of using label",
			}

			enrichMaintainerDeprecatedFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, 1)
				assert.Equal(t, tt.wantText, v.SuggestedFix.Edits[0].NewText)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestConsistentInstructionCasingFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		source      string
		message     string
		wantFix     bool
		wantNewText string
	}{
		{
			name:        "lowercase run should become uppercase",
			source:      "run echo hello",
			message:     "Command 'run' should match the case of the command majority (uppercase)",
			wantFix:     true,
			wantNewText: "RUN",
		},
		{
			name:        "lowercase workdir should become uppercase",
			source:      "workdir /app",
			message:     "Command 'workdir' should match the case of the command majority (uppercase)",
			wantFix:     true,
			wantNewText: "WORKDIR",
		},
		{
			name:        "uppercase COPY should become lowercase",
			source:      "COPY . /app",
			message:     "Command 'COPY' should match the case of the command majority (lowercase)",
			wantFix:     true,
			wantNewText: "copy",
		},
		{
			name:        "lowercase from should become uppercase",
			source:      "from alpine AS builder",
			message:     "Command 'from' should match the case of the command majority (uppercase)",
			wantFix:     true,
			wantNewText: "FROM",
		},
		{
			name:        "uppercase FROM should become lowercase",
			source:      "FROM alpine AS builder",
			message:     "Command 'FROM' should match the case of the command majority (lowercase)",
			wantFix:     true,
			wantNewText: "from",
		},
		{
			name:    "invalid message format",
			source:  "run echo hello",
			message: "Some other message format",
			wantFix: false,
		},
		{
			name:        "mixed case cmd should become uppercase",
			source:      "Cmd echo hello",
			message:     "Command 'Cmd' should match the case of the command majority (uppercase)",
			wantFix:     true,
			wantNewText: "CMD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
				RuleCode: rules.BuildKitRulePrefix + "ConsistentInstructionCasing",
				Message:  tt.message,
			}

			enrichConsistentInstructionCasingFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, 1)
				assert.Equal(t, tt.wantNewText, v.SuggestedFix.Edits[0].NewText)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestFindFROMBaseName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		line      string
		wantStart int
		wantEnd   int
	}{
		{
			name:      "simple FROM",
			line:      "FROM alpine",
			wantStart: 5,
			wantEnd:   11,
		},
		{
			name:      "FROM with tag",
			line:      "FROM alpine:3.18",
			wantStart: 5,
			wantEnd:   16,
		},
		{
			name:      "FROM with AS",
			line:      "FROM alpine AS builder",
			wantStart: 5,
			wantEnd:   11,
		},
		{
			name:      "FROM with platform",
			line:      "FROM --platform=linux/amd64 alpine",
			wantStart: 28,
			wantEnd:   34,
		},
		{
			name:      "FROM stage reference",
			line:      "FROM Builder",
			wantStart: 5,
			wantEnd:   12,
		},
		{
			name:      "not a FROM line",
			line:      "RUN echo hello",
			wantStart: -1,
			wantEnd:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, end := findFROMBaseName([]byte(tt.line))
			assert.Equal(t, tt.wantStart, start, "start")
			assert.Equal(t, tt.wantEnd, end, "end")
		})
	}
}

func TestLegacyKeyValueFormatFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   string
		message  string
		wantFix  bool
		wantText string
	}{
		{
			name:     "simple ENV key value",
			source:   "ENV foo bar",
			message:  `"ENV key=value" should be used instead of legacy "ENV key value" format`,
			wantFix:  true,
			wantText: "foo=bar",
		},
		{
			name:     "ENV with multi-word value",
			source:   "ENV MY_VAR hello world",
			message:  `"ENV key=value" should be used instead of legacy "ENV key value" format`,
			wantFix:  true,
			wantText: `MY_VAR="hello world"`,
		},
		{
			name:     "LABEL with multi-word value",
			source:   "LABEL maintainer John Doe",
			message:  `"LABEL key=value" should be used instead of legacy "LABEL key value" format`,
			wantFix:  true,
			wantText: `maintainer="John Doe"`,
		},
		{
			name:     "LABEL simple",
			source:   "LABEL version 1.0",
			message:  `"LABEL key=value" should be used instead of legacy "LABEL key value" format`,
			wantFix:  true,
			wantText: "version=1.0",
		},
		{
			name:     "ENV with extra whitespace",
			source:   "ENV   foo   bar",
			message:  `"ENV key=value" should be used instead of legacy "ENV key value" format`,
			wantFix:  true,
			wantText: "foo=bar",
		},
		{
			name:     "lowercase env",
			source:   "env foo bar",
			message:  `"ENV key=value" should be used instead of legacy "ENV key value" format`,
			wantFix:  true,
			wantText: "foo=bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
				RuleCode: rules.BuildKitRulePrefix + "LegacyKeyValueFormat",
				Message:  tt.message,
			}

			enrichLegacyKeyValueFormatFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, 1)
				assert.Equal(t, tt.wantText, v.SuggestedFix.Edits[0].NewText)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestInvalidDefinitionDescriptionFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		source        string
		violationLine int // 1-based line where BuildKit reports the violation (the FROM/ARG line)
		wantFix       bool
		wantEditLine  int    // 1-based line number where edit should be applied
		wantEditCol   int    // Column where edit should be applied
		wantNewText   string // Expected newline to insert
	}{
		{
			name:          "comment before FROM with stage name",
			source:        "# Test comment\nFROM alpine AS builder",
			violationLine: 2,
			wantFix:       true,
			wantEditLine:  1,
			wantEditCol:   14, // End of "# Test comment"
			wantNewText:   "\n",
		},
		{
			name:          "comment before ARG",
			source:        "# Build argument\nARG VERSION=1.0",
			violationLine: 2,
			wantFix:       true,
			wantEditLine:  1,
			wantEditCol:   16, // End of "# Build argument"
			wantNewText:   "\n",
		},
		{
			name:          "CRLF line endings",
			source:        "# Test comment\r\nFROM alpine AS builder",
			violationLine: 2,
			wantFix:       true,
			wantEditLine:  1,
			wantEditCol:   14,
			wantNewText:   "\r\n",
		},
		{
			name:          "violation on first line - no fix possible",
			source:        "FROM alpine AS builder",
			violationLine: 1,
			wantFix:       false,
		},
		{
			name:          "previous line is not a comment",
			source:        "RUN echo hello\nFROM alpine AS builder",
			violationLine: 2,
			wantFix:       false,
		},
		{
			name:          "previous line is empty",
			source:        "\nFROM alpine AS builder",
			violationLine: 2,
			wantFix:       false,
		},
		{
			name:          "comment with trailing whitespace",
			source:        "# Comment with spaces   \nFROM alpine AS builder",
			violationLine: 2,
			wantFix:       true,
			wantEditLine:  1,
			wantEditCol:   24, // End of "# Comment with spaces   "
			wantNewText:   "\n",
		},
		{
			name: "multiple violations in multiline file",
			source: `# check=experimental=InvalidDefinitionDescription
# bar this is the bar
ARG foo=bar
# BasE this is the BasE image
FROM scratch AS base`,
			violationLine: 3, // ARG foo=bar
			wantFix:       true,
			wantEditLine:  2,
			wantEditCol:   21, // End of "# bar this is the bar"
			wantNewText:   "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", tt.violationLine, 0, tt.violationLine, 0),
				RuleCode: rules.BuildKitRulePrefix + "InvalidDefinitionDescription",
				Message:  "Comment for FROM should follow the format: `# builder <description>`",
			}

			enrichInvalidDefinitionDescriptionFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, 1)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)

				edit := v.SuggestedFix.Edits[0]
				assert.Equal(t, tt.wantEditLine, edit.Location.Start.Line, "edit line")
				assert.Equal(t, tt.wantEditCol, edit.Location.Start.Column, "edit start column")
				assert.Equal(t, tt.wantEditCol, edit.Location.End.Column, "edit end column")
				assert.Equal(t, tt.wantNewText, edit.NewText, "newText")
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestExposeProtoCasingFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    string
		message   string
		wantFix   bool
		wantEdits int
		wantTexts []string
	}{
		{
			name:      "uppercase TCP",
			source:    "EXPOSE 8080/TCP",
			message:   "Defined protocol '8080/TCP' in EXPOSE instruction should be lowercase",
			wantFix:   true,
			wantEdits: 1,
			wantTexts: []string{"tcp"},
		},
		{
			name:      "uppercase UDP",
			source:    "EXPOSE 53/UDP",
			message:   "Defined protocol '53/UDP' in EXPOSE instruction should be lowercase",
			wantFix:   true,
			wantEdits: 1,
			wantTexts: []string{"udp"},
		},
		{
			name:      "mixed case Tcp",
			source:    "EXPOSE 8080/Tcp",
			message:   "Defined protocol '8080/Tcp' in EXPOSE instruction should be lowercase",
			wantFix:   true,
			wantEdits: 1,
			wantTexts: []string{"tcp"},
		},
		{
			name:      "multiple ports with uppercase protocols",
			source:    "EXPOSE 80/TCP 443/UDP",
			message:   "Defined protocol '80/TCP' in EXPOSE instruction should be lowercase",
			wantFix:   true,
			wantEdits: 2,
			wantTexts: []string{"tcp", "udp"},
		},
		{
			name:      "port range with uppercase protocol",
			source:    "EXPOSE 8080-8090/TCP",
			message:   "Defined protocol '8080-8090/TCP' in EXPOSE instruction should be lowercase",
			wantFix:   true,
			wantEdits: 1,
			wantTexts: []string{"tcp"},
		},
		{
			name:    "already lowercase",
			source:  "EXPOSE 8080/tcp",
			message: "Defined protocol '8080/tcp' in EXPOSE instruction should be lowercase",
			wantFix: false,
		},
		{
			name:    "no protocol",
			source:  "EXPOSE 8080",
			message: "Defined protocol '8080' in EXPOSE instruction should be lowercase",
			wantFix: false,
		},
		{
			name:      "only one port has uppercase",
			source:    "EXPOSE 80/tcp 443/UDP",
			message:   "Defined protocol '443/UDP' in EXPOSE instruction should be lowercase",
			wantFix:   true,
			wantEdits: 1,
			wantTexts: []string{"udp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
				RuleCode: rules.BuildKitRulePrefix + "ExposeProtoCasing",
				Message:  tt.message,
			}

			enrichExposeProtoCasingFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, tt.wantEdits)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)
				for i, wantText := range tt.wantTexts {
					assert.Equal(t, wantText, v.SuggestedFix.Edits[i].NewText)
				}
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}

func TestMultipleInstructionsDisallowedFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   string
		message  string
		wantFix  bool
		wantText string
	}{
		{
			name:     "CMD shell form",
			source:   `CMD echo "hello"`,
			message:  "Multiple CMD instructions should not be used in the same stage because only the last one will be used",
			wantFix:  true,
			wantText: `# [commented out by tally - Docker will ignore all but last CMD]: CMD echo "hello"`,
		},
		{
			name:     "CMD exec form",
			source:   `CMD ["echo", "hello"]`,
			message:  "Multiple CMD instructions should not be used in the same stage because only the last one will be used",
			wantFix:  true,
			wantText: `# [commented out by tally - Docker will ignore all but last CMD]: CMD ["echo", "hello"]`,
		},
		{
			name:     "ENTRYPOINT shell form",
			source:   `ENTRYPOINT /bin/bash`,
			message:  "Multiple ENTRYPOINT instructions should not be used in the same stage because only the last one will be used",
			wantFix:  true,
			wantText: `# [commented out by tally - Docker will ignore all but last ENTRYPOINT]: ENTRYPOINT /bin/bash`,
		},
		{
			name:     "ENTRYPOINT exec form",
			source:   `ENTRYPOINT ["/bin/sh", "-c"]`,
			message:  "Multiple ENTRYPOINT instructions should not be used in the same stage because only the last one will be used",
			wantFix:  true,
			wantText: `# [commented out by tally - Docker will ignore all but last ENTRYPOINT]: ENTRYPOINT ["/bin/sh", "-c"]`,
		},
		{
			name:    "unrecognized message format",
			source:  `CMD echo hello`,
			message: "Some other message",
			wantFix: false,
		},
		{
			name:     "CMD with leading whitespace",
			source:   `  CMD echo hello`,
			message:  "Multiple CMD instructions should not be used in the same stage because only the last one will be used",
			wantFix:  true,
			wantText: `# [commented out by tally - Docker will ignore all but last CMD]:   CMD echo hello`,
		},
		{
			name:     "HEALTHCHECK CMD form",
			source:   `HEALTHCHECK CMD /bin/check`,
			message:  "Multiple HEALTHCHECK instructions should not be used in the same stage because only the last one will be used",
			wantFix:  true,
			wantText: `# [commented out by tally - Docker will ignore all but last HEALTHCHECK]: HEALTHCHECK CMD /bin/check`,
		},
		{
			name:    "HEALTHCHECK with options",
			source:  `HEALTHCHECK --interval=30s CMD curl -f http://localhost/`,
			message: "Multiple HEALTHCHECK instructions should not be used in the same stage because only the last one will be used",
			wantFix: true,
			wantText: "# [commented out by tally - Docker will ignore all but last HEALTHCHECK]: " +
				"HEALTHCHECK --interval=30s CMD curl -f http://localhost/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := []byte(tt.source)
			v := rules.Violation{
				Location: rules.NewRangeLocation("test.Dockerfile", 1, 0, 1, len(tt.source)),
				RuleCode: rules.BuildKitRulePrefix + "MultipleInstructionsDisallowed",
				Message:  tt.message,
			}

			enrichMultipleInstructionsDisallowedFix(&v, source)

			if tt.wantFix {
				require.NotNil(t, v.SuggestedFix, "expected a fix")
				assert.Len(t, v.SuggestedFix.Edits, 1)
				assert.Equal(t, tt.wantText, v.SuggestedFix.Edits[0].NewText)
				assert.Equal(t, rules.FixSafe, v.SuggestedFix.Safety)
				assert.True(t, v.SuggestedFix.IsPreferred)
			} else {
				assert.Nil(t, v.SuggestedFix, "expected no fix")
			}
		})
	}
}
