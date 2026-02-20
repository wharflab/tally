package fix

import (
	"context"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// TestLineShiftAcrossPriorities verifies that when a lower-priority sync edit
// inserts a new line (e.g., DL4006 SHELL insertion at priority 0), higher-priority
// sync edits (e.g., chain-split at priority 97) have their line numbers adjusted.
// Without this fix, chain-split targets the wrong line — the line before the actual
// RUN instruction — producing orphaned continuation characters.
func TestLineShiftAcrossPriorities(t *testing.T) {
	t.Parallel()
	input := "FROM ubuntu\n" + // line 1
		"RUN cat /etc/os-release | grep VERSION\n" + // line 2
		"ENTRYPOINT [\"bash\"]\n" + // line 3
		"RUN apt-get update  && apt-get install -y vim  && apt-get clean  && rm -rf /var/lib/apt/lists/*\n" + // line 4
		"COPY foo bar\n" // line 5

	sm := sourcemap.New([]byte(input))

	dl4006Edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 0, 2, 0),
		NewText:  "SHELL [\"/bin/bash\", \"-o\", \"pipefail\", \"-c\"]\n",
	}

	lineContent := sm.Line(3)
	var chainEdits []rules.TextEdit
	col := 0
	for {
		idx := strings.Index(lineContent[col:], "  && ")
		if idx == -1 {
			break
		}
		pos := col + idx
		chainEdits = append(chainEdits, rules.TextEdit{
			Location: rules.NewRangeLocation("Dockerfile", 4, pos, 4, pos+5),
			NewText:  " \\\n\t&& ",
		})
		col = pos + 5
	}

	allViolations := []rules.Violation{
		rules.NewViolation(
			rules.NewRangeLocation("Dockerfile", 2, 0, 2, 0), "DL4006", "SHELL", rules.SeverityWarning,
		).WithSuggestedFix(&rules.SuggestedFix{
			Description: "Add SHELL with -o pipefail before RUN",
			Safety:      rules.FixSafe,
			Priority:    0,
			Edits:       []rules.TextEdit{dl4006Edit},
		}),
		rules.NewViolation(
			rules.NewRangeLocation("Dockerfile", 4, 0, 4, len(lineContent)),
			"tally/newline-per-chained-call", "split chains", rules.SeverityStyle,
		).WithSuggestedFix(&rules.SuggestedFix{
			Description: "Split chains",
			Safety:      rules.FixSafe,
			Priority:    97,
			Edits:       chainEdits,
		}),
	}

	fixer := &Fixer{SafetyThreshold: rules.FixUnsafe}
	fixResult, err := fixer.Apply(context.Background(), allViolations, map[string][]byte{
		"Dockerfile": []byte(input),
	})
	if err != nil {
		t.Fatalf("fix error: %v", err)
	}

	modified := string(fixResult.Changes["Dockerfile"].ModifiedContent)

	// Chain-split should target the correct RUN (now at line 5 after SHELL insertion)
	if strings.Contains(modified, " \\ \\") || strings.Contains(modified, "\t&& \n") {
		t.Errorf("chain-split targeted wrong line due to missing line-shift adjustment:\n%s", modified)
	}
	if !strings.Contains(modified, "apt-get update \\\n") {
		t.Errorf("expected chain-split continuation on apt-get update line:\n%s", modified)
	}
	if !strings.Contains(modified, "SHELL") {
		t.Errorf("expected SHELL instruction")
	}
}

// TestLineShiftWithColumnShift verifies that line-shift and column-shift tracking
// compose correctly. DL3027 (priority 0) changes "apt install" to "apt-get install"
// (+4 chars column shift), DL4006 (priority 0) inserts SHELL (+1 line shift), and
// chain-split (priority 97) must adjust both line AND column positions.
func TestLineShiftWithColumnShift(t *testing.T) {
	t.Parallel()
	input := "FROM ubuntu\n" + // line 1
		"RUN cat /etc/os-release | grep VERSION\n" + // line 2
		"ENTRYPOINT [\"bash\"]\n" + // line 3
		"RUN apt-get update  && apt install -y vim  && apt-get clean  && rm -rf /var/lib/apt/lists/*\n" + // line 4
		"COPY foo bar\n" // line 5

	sm := sourcemap.New([]byte(input))
	lineContent := sm.Line(3)

	dl4006Edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 0, 2, 0),
		NewText:  "SHELL [\"/bin/bash\", \"-o\", \"pipefail\", \"-c\"]\n",
	}

	aptIdx := strings.Index(lineContent, "apt install")
	dl3027Edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 4, aptIdx, 4, aptIdx+len("apt install")),
		NewText:  "apt-get install",
	}

	var chainEdits []rules.TextEdit
	col := 0
	for {
		idx := strings.Index(lineContent[col:], "  && ")
		if idx == -1 {
			break
		}
		pos := col + idx
		chainEdits = append(chainEdits, rules.TextEdit{
			Location: rules.NewRangeLocation("Dockerfile", 4, pos, 4, pos+5),
			NewText:  " \\\n\t&& ",
		})
		col = pos + 5
	}

	allViolations := []rules.Violation{
		rules.NewViolation(
			rules.NewRangeLocation("Dockerfile", 2, 0, 2, 0), "DL4006", "SHELL", rules.SeverityWarning,
		).WithSuggestedFix(&rules.SuggestedFix{
			Description: "Add SHELL",
			Safety:      rules.FixSafe,
			Edits:       []rules.TextEdit{dl4006Edit},
		}),
		rules.NewViolation(
			rules.NewRangeLocation("Dockerfile", 4, aptIdx, 4, aptIdx+11), "DL3027", "apt", rules.SeverityWarning,
		).WithSuggestedFix(&rules.SuggestedFix{
			Description: "apt→apt-get",
			Safety:      rules.FixSafe,
			Edits:       []rules.TextEdit{dl3027Edit},
		}),
		rules.NewViolation(
			rules.NewRangeLocation("Dockerfile", 4, 0, 4, len(lineContent)),
			"tally/newline-per-chained-call", "split chains", rules.SeverityStyle,
		).WithSuggestedFix(&rules.SuggestedFix{
			Description: "Split chains",
			Safety:      rules.FixSafe,
			Priority:    97,
			Edits:       chainEdits,
		}),
	}

	fixer := &Fixer{SafetyThreshold: rules.FixUnsafe}
	fixResult, err := fixer.Apply(context.Background(), allViolations, map[string][]byte{
		"Dockerfile": []byte(input),
	})
	if err != nil {
		t.Fatalf("fix error: %v", err)
	}

	modified := string(fixResult.Changes["Dockerfile"].ModifiedContent)

	if strings.Contains(modified, " \\ \\") || strings.Contains(modified, "\t&& \n") {
		t.Errorf("chain-split targeted wrong line:\n%s", modified)
	}
	if !strings.Contains(modified, "apt-get update \\\n") {
		t.Errorf("expected chain-split continuation:\n%s", modified)
	}
	if !strings.Contains(modified, "apt-get install -y vim") {
		t.Errorf("expected DL3027 to fix 'apt install' → 'apt-get install':\n%s", modified)
	}
	if strings.Contains(modified, "apt install") {
		t.Errorf("DL3027 fix should have replaced 'apt install':\n%s", modified)
	}
}
