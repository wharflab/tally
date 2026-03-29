package windows

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoChownFlagRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoChownFlagRule().Metadata())
}

func TestNoChownFlagRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoChownFlagRule(), []testutil.RuleTestCase{
		{
			Name: "copy chown on windows servercore",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY --chown=app:app src/ C:/app/
`,
			WantViolations: 1,
			WantCodes:      []string{NoChownFlagRuleCode},
			WantMessages:   []string{"COPY --chown=app:app is silently ignored on Windows"},
		},
		{
			Name: "add chown on windows servercore",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
ADD --chown=1000:1000 archive.tar.gz C:/app/
`,
			WantViolations: 1,
			WantCodes:      []string{NoChownFlagRuleCode},
			WantMessages:   []string{"ADD --chown=1000:1000 is silently ignored on Windows"},
		},
		{
			Name: "copy chown on windows nanoserver",
			Content: `FROM mcr.microsoft.com/windows/nanoserver:ltsc2022
COPY --chown=ContainerUser src/ C:/app/
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY --chown=ContainerUser is silently ignored"},
		},
		{
			Name: "copy without chown on windows no violation",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY src/ C:/app/
`,
			WantViolations: 0,
		},
		{
			Name: "copy chown on linux no violation",
			Content: `FROM alpine:3.20
COPY --chown=app:app src/ /app/
`,
			WantViolations: 0,
		},
		{
			Name: "copy chown on ubuntu no violation",
			Content: `FROM ubuntu:22.04
COPY --chown=1000:1000 . /app/
`,
			WantViolations: 0,
		},
		{
			Name: "mixed stages only windows flagged",
			Content: `FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
COPY --chown=app:app . /src/
RUN dotnet publish -c Release -o /app

FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY --chown=app:app --from=build /app C:/app/
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY --chown=app:app is silently ignored on Windows"},
		},
		{
			Name: "multiple copy add with chown in same windows stage",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY --chown=app src/ C:/app/
ADD --chown=app config.tar.gz C:/config/
`,
			WantViolations: 2,
		},
		{
			Name: "copy chown with chmod on windows only chown flagged",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY --chown=app --chmod=755 src/ C:/app/
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY --chown=app is silently ignored"},
		},
		{
			Name: "copy from with chown on windows",
			Content: `FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
RUN dotnet publish -c Release -o /app

FROM mcr.microsoft.com/windows/servercore:ltsc2022
COPY --from=build --chown=app /app C:/app/
`,
			WantViolations: 1,
		},
		{
			Name: "platform flag windows with chown",
			Content: `FROM --platform=windows/amd64 mcr.microsoft.com/dotnet/sdk:8.0
COPY --chown=app:app . C:\src\
`,
			WantViolations: 1,
		},
		{
			Name: "empty scratch no violation",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "windows stage without copy or add no violation",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command echo hello
CMD ["cmd", "/C", "echo", "hi"]
`,
			WantViolations: 0,
		},
	})
}

func TestNoChownFlagRule_Fix(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nCOPY --chown=app:app src/ C:/app/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoChownFlagRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	if v.SuggestedFix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
	}
	if !v.SuggestedFix.IsPreferred {
		t.Error("expected IsPreferred to be true")
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}

	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "" {
		t.Errorf("NewText = %q, want empty string", edit.NewText)
	}
	// --chown=app:app occupies columns 5-20 (including trailing space)
	// "COPY --chown=app:app src/ C:/app/"
	//  01234567890123456789012
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2", edit.Location.Start.Line)
	}
	if edit.Location.Start.Column != 5 {
		t.Errorf("edit Start.Column = %d, want 5", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 21 {
		t.Errorf("edit End.Column = %d, want 21", edit.Location.End.Column)
	}
}

func TestNoChownFlagRule_FixAddCommand(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nADD --chown=1000:1000 archive.tar.gz C:/app/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoChownFlagRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	if fix.Edits[0].NewText != "" {
		t.Errorf("NewText = %q, want empty", fix.Edits[0].NewText)
	}
	// "ADD --chown=1000:1000 archive.tar.gz C:/app/"
	//  0123456789012345678901
	if fix.Edits[0].Location.Start.Column != 4 {
		t.Errorf("edit Start.Column = %d, want 4", fix.Edits[0].Location.Start.Column)
	}
	if fix.Edits[0].Location.End.Column != 22 {
		t.Errorf("edit End.Column = %d, want 22", fix.Edits[0].Location.End.Column)
	}
}

func TestNoChownFlagRule_FixWithOtherFlags(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nCOPY --from=build --chown=app /app C:/app/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoChownFlagRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	// "COPY --from=build --chown=app /app C:/app/"
	//  0         1         2
	//  0123456789012345678901234567890
	// --chown=app starts at 18, ends at 29 (with trailing space)
	if fix.Edits[0].Location.Start.Column != 18 {
		t.Errorf("edit Start.Column = %d, want 18", fix.Edits[0].Location.Start.Column)
	}
	if fix.Edits[0].Location.End.Column != 30 {
		t.Errorf("edit End.Column = %d, want 30", fix.Edits[0].Location.End.Column)
	}
}

func TestNoChownFlagRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nRUN echo hello\nCOPY --chown=app src/ C:/app/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoChownFlagRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Location.Start.Line != 3 {
		t.Errorf("Start.Line = %d, want 3", violations[0].Location.Start.Line)
	}
}

func TestFindChownFlagRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		wantStart int
		wantEnd   int
		wantFound bool
	}{
		{
			name:      "simple chown",
			line:      "COPY --chown=app src/ /app/",
			wantStart: 5,
			wantEnd:   17,
			wantFound: true,
		},
		{
			name:      "chown with group",
			line:      "COPY --chown=app:app src/ /app/",
			wantStart: 5,
			wantEnd:   21,
			wantFound: true,
		},
		{
			name:      "chown with numeric ids",
			line:      "ADD --chown=1000:1000 archive.tar.gz /app/",
			wantStart: 4,
			wantEnd:   22,
			wantFound: true,
		},
		{
			name:      "chown after other flags",
			line:      "COPY --from=build --chown=app /app /dest",
			wantStart: 18,
			wantEnd:   30,
			wantFound: true,
		},
		{
			name:      "quoted chown double",
			line:      `COPY --chown="app:group" src/ /app/`,
			wantStart: 5,
			wantEnd:   25,
			wantFound: true,
		},
		{
			name:      "quoted chown single",
			line:      `COPY --chown='app' src/ /app/`,
			wantStart: 5,
			wantEnd:   19,
			wantFound: true,
		},
		{
			name:      "no chown flag",
			line:      "COPY src/ /app/",
			wantStart: 0,
			wantEnd:   0,
			wantFound: false,
		},
		{
			name:      "chown at end of line no trailing space",
			line:      "COPY --chown=app",
			wantStart: 5,
			wantEnd:   16,
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, end, found := findChownFlagRange(tt.line)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
		})
	}
}
