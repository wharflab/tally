package shellcheck

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

type sc1040TestCase struct {
	name      string
	script    string
	wantCount int
	wantLine  int
	wantCol   int
	wantEdits []expectedEdit
}

type expectedEdit struct {
	start   int
	end     int
	newText string
}

func TestNativeOwnedShellcheckExcludeCodes(t *testing.T) {
	t.Parallel()

	exclude := nativeOwnedShellcheckExcludeCodes()
	if len(exclude) != 1 {
		t.Fatalf("exclude codes count = %d, want 1 (%v)", len(exclude), exclude)
	}
	if exclude[0] != "SC1040" {
		t.Fatalf("exclude[0] = %q, want %q", exclude[0], "SC1040")
	}
}

func TestSC1040UpstreamReadHereDocVectors(t *testing.T) {
	t.Parallel()

	origin := 20
	tests := []sc1040TestCase{
		{name: "prop_readHereDoc", script: "cat << foo\nlol\ncow\nfoo", wantCount: 0},
		{name: "prop_readHereDoc2", script: "cat <<- EOF\n  cow\n  EOF", wantCount: 0},
		{name: "prop_readHereDoc3", script: "cat << foo\n$\"\nfoo", wantCount: 0},
		{name: "prop_readHereDoc4", script: "cat << foo\n`\nfoo", wantCount: 0},
		{name: "prop_readHereDoc5", script: "cat <<- !foo\nbar\n!foo", wantCount: 0},
		{name: "prop_readHereDoc6", script: "cat << foo\\ bar\ncow\nfoo bar", wantCount: 0},
		{name: "prop_readHereDoc7", script: "cat << foo\n\\$(f ())\nfoo", wantCount: 0},
		{name: "prop_readHereDoc8", script: "cat <<foo>>bar\netc\nfoo", wantCount: 0},
		{name: "prop_readHereDoc9", script: "if true; then cat << foo; fi\nbar\nfoo\n", wantCount: 0},
		{name: "prop_readHereDoc10", script: "if true; then cat << foo << bar; fi\nfoo\nbar\n", wantCount: 0},
		{name: "prop_readHereDoc11", script: "cat << foo $(\nfoo\n)lol\nfoo\n", wantCount: 0},
		{name: "prop_readHereDoc12", script: "cat << foo|cat\nbar\nfoo", wantCount: 0},
		{name: "prop_readHereDoc13", script: "cat <<'#!'\nHello World\n#!\necho Done", wantCount: 0},
		{name: "prop_readHereDoc14", script: "cat << foo\nbar\nfoo \n", wantCount: 0},
		{name: "prop_readHereDoc15", script: "cat <<foo\nbar\nfoo bar\nfoo", wantCount: 0},
		{name: "prop_readHereDoc16", script: "cat <<- ' foo'\nbar\n foo\n", wantCount: 0},
		{
			name:      "prop_readHereDoc17",
			script:    "cat <<- ' foo'\nbar\n  foo\n foo\n",
			wantCount: 1,
			wantLine:  origin + 2,
			wantCol:   0,
			wantEdits: []expectedEdit{{start: 0, end: 1, newText: ""}},
		},
		{name: "prop_readHereDoc18", script: "cat <<'\"foo'\nbar\n\"foo\n", wantCount: 0},
		{name: "prop_readHereDoc20", script: "cat << foo\n  foo\n()\nfoo\n", wantCount: 0},
		{name: "prop_readHereDoc21", script: "# shellcheck disable=SC1039\ncat << foo\n  foo\n()\nfoo\n", wantCount: 0},
		{name: "prop_readHereDoc22", script: "cat << foo\r\ncow\r\nfoo\r\n", wantCount: 0},
		{name: "prop_readHereDoc23", script: "cat << foo \r\ncow\r\nfoo\r\n", wantCount: 0},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runNativeShellcheckChecks(
				"Dockerfile",
				rules.NewLineLocation("Dockerfile", origin),
				scriptMapping{Script: tc.script, OriginStartLine: origin, FallbackLine: origin},
			)
			assertSC1040Case(t, got, tc)
		})
	}
}

func TestSC1040FixPreservesTabsAndRemovesSpaces(t *testing.T) {
	t.Parallel()

	script := "cat <<-EOF\nbody\n\t \tEOF\nEOF\n"
	origin := 30
	violations := runNativeShellcheckChecks(
		"Dockerfile",
		rules.NewLineLocation("Dockerfile", origin),
		scriptMapping{Script: script, OriginStartLine: origin, FallbackLine: origin},
	)
	if len(violations) != 1 {
		t.Fatalf("violations count = %d, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatalf("expected fix, got %+v", fix)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("fix edits count = %d, want 1", len(fix.Edits))
	}
	if fix.Edits[0].Location.Start.Column != 1 || fix.Edits[0].Location.End.Column != 2 {
		t.Fatalf("fix columns = %d-%d, want 1-2", fix.Edits[0].Location.Start.Column, fix.Edits[0].Location.End.Column)
	}
	if fix.Edits[0].NewText != "" {
		t.Fatalf("fix text = %q, want empty", fix.Edits[0].NewText)
	}
}

func TestSC1040FixProducesSeparateEditsForSpaceRuns(t *testing.T) {
	t.Parallel()

	script := "cat <<-EOF\nbody\n\t  \t   EOF\nEOF\n"
	origin := 40
	violations := runNativeShellcheckChecks(
		"Dockerfile",
		rules.NewLineLocation("Dockerfile", origin),
		scriptMapping{Script: script, OriginStartLine: origin, FallbackLine: origin},
	)
	if len(violations) != 1 {
		t.Fatalf("violations count = %d, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatalf("expected fix, got %+v", fix)
	}
	if len(fix.Edits) != 2 {
		t.Fatalf("fix edits count = %d, want 2", len(fix.Edits))
	}

	first := fix.Edits[0]
	if first.Location.Start.Column != 1 || first.Location.End.Column != 3 || first.NewText != "" {
		t.Fatalf("first edit = %+v, want columns 1-3 with empty replacement", first)
	}
	second := fix.Edits[1]
	if second.Location.Start.Column != 4 || second.Location.End.Column != 7 || second.NewText != "" {
		t.Fatalf("second edit = %+v, want columns 4-7 with empty replacement", second)
	}
}

func assertSC1040Case(t *testing.T, got []rules.Violation, tc sc1040TestCase) {
	t.Helper()

	if len(got) != tc.wantCount {
		t.Fatalf("violations count = %d, want %d: %+v", len(got), tc.wantCount, got)
	}
	if tc.wantCount == 0 {
		return
	}

	assertSC1040Violation(t, got[0], tc)
}

func assertSC1040Violation(t *testing.T, v rules.Violation, tc sc1040TestCase) {
	t.Helper()

	if v.RuleCode != sc1040RuleCode {
		t.Fatalf("rule = %q, want %q", v.RuleCode, sc1040RuleCode)
	}
	if v.Message != sc1040Message {
		t.Fatalf("message = %q, want %q", v.Message, sc1040Message)
	}
	if v.Severity != rules.SeverityError {
		t.Fatalf("severity = %q, want %q", v.Severity, rules.SeverityError)
	}
	if v.DocURL != rules.TallyDocURL(sc1040RuleCode) {
		t.Fatalf("doc url = %q, want %q", v.DocURL, rules.TallyDocURL(sc1040RuleCode))
	}
	if v.Location.Start.Line != tc.wantLine {
		t.Fatalf("line = %d, want %d", v.Location.Start.Line, tc.wantLine)
	}
	if v.Location.Start.Column != tc.wantCol {
		t.Fatalf("start column = %d, want %d", v.Location.Start.Column, tc.wantCol)
	}
	if v.SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}
	if v.SuggestedFix.Safety != rules.FixSafe {
		t.Fatalf("fix safety = %v, want %v", v.SuggestedFix.Safety, rules.FixSafe)
	}
	if !v.SuggestedFix.IsPreferred {
		t.Fatal("expected preferred fix")
	}
	if len(v.SuggestedFix.Edits) != len(tc.wantEdits) {
		t.Fatalf("fix edits count = %d, want %d", len(v.SuggestedFix.Edits), len(tc.wantEdits))
	}

	for i := range v.SuggestedFix.Edits {
		edit := v.SuggestedFix.Edits[i]
		want := tc.wantEdits[i]
		if edit.Location.Start.Line != tc.wantLine || edit.Location.End.Line != tc.wantLine {
			t.Fatalf("fix line range = %d-%d, want %d", edit.Location.Start.Line, edit.Location.End.Line, tc.wantLine)
		}
		if edit.Location.Start.Column != want.start || edit.Location.End.Column != want.end {
			t.Fatalf(
				"fix[%d] columns = %d-%d, want %d-%d",
				i,
				edit.Location.Start.Column,
				edit.Location.End.Column,
				want.start,
				want.end,
			)
		}
		if edit.NewText != want.newText {
			t.Fatalf("fix[%d] new text = %q, want %q", i, edit.NewText, want.newText)
		}
	}
}
