package hadolint

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/semantic"
)

func TestDL3022_CopyFromUndefinedAlias(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		shouldFail bool
	}{
		{
			name:       "warn on missing alias",
			dockerfile: "FROM scratch\nCOPY --from=foo bar .",
			shouldFail: true,
		},
		{
			name: "warn on alias defined after",
			dockerfile: strings.Join([]string{
				"FROM scratch",
				"COPY --from=build foo .",
				"FROM node as build",
				"RUN baz",
			}, "\n"),
			shouldFail: true,
		},
		{
			name: "don't warn on correctly defined aliases",
			dockerfile: strings.Join([]string{
				"FROM scratch as build",
				"RUN foo",
				"FROM node",
				"COPY --from=build foo .",
				"RUN baz",
			}, "\n"),
			shouldFail: false,
		},
		{
			name:       "don't warn on external images",
			dockerfile: `COPY --from=haskell:latest bar .`,
			shouldFail: false,
		},
		{
			name:       "warn on out-of-range numeric stage index",
			dockerfile: "FROM scratch\nCOPY --from=1 bar .",
			shouldFail: true,
		},
		{
			name:       "warn on self-referencing numeric stage index",
			dockerfile: "FROM scratch\nCOPY --from=0 bar .",
			shouldFail: true,
		},
		{
			name: "don't warn on valid stage count with named stage",
			dockerfile: strings.Join([]string{
				"FROM scratch as build",
				"RUN foo",
				"FROM node",
				"COPY --from=0 foo .",
			}, "\n"),
			shouldFail: false,
		},
		{
			name: "don't warn on valid stage count with unnamed stage",
			dockerfile: strings.Join([]string{
				"FROM scratch",
				"RUN foo",
				"FROM node",
				"COPY --from=0 foo .",
			}, "\n"),
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := dockerfile.Parse(strings.NewReader(tt.dockerfile), nil)
			if err != nil {
				t.Fatalf("failed to parse Dockerfile: %v", err)
			}

			model := semantic.NewModel(result, nil, "Dockerfile")
			issues := model.ConstructionIssues()

			var foundDL3022 bool
			for _, issue := range issues {
				if issue.Code == DL3022Code {
					foundDL3022 = true
					break
				}
			}

			if tt.shouldFail && !foundDL3022 {
				t.Errorf("expected DL3022 violation but none found")
			}
			if !tt.shouldFail && foundDL3022 {
				t.Errorf("unexpected DL3022 violation")
			}
		})
	}
}
