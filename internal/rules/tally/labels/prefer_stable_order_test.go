package labels

import (
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wharflab/tally/internal/facts"
	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferStableOrderRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewPreferStableOrderRule().Metadata()
	if meta.Code != PreferStableOrderRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, PreferStableOrderRuleCode)
	}
	if meta.DefaultSeverity.String() != "info" {
		t.Fatalf("DefaultSeverity = %s, want info", meta.DefaultSeverity)
	}
	if meta.Category != "style" {
		t.Fatalf("Category = %q, want style", meta.Category)
	}
	if meta.FixPriority != 95 {
		t.Fatalf("FixPriority = %d, want 95", meta.FixPriority)
	}
	if meta.IsExperimental {
		t.Fatal("IsExperimental should be false")
	}
}

func TestPreferStableOrderRule_Schema(t *testing.T) {
	t.Parallel()

	schema := NewPreferStableOrderRule().Schema()
	if schema == nil {
		t.Fatal("Schema() returned nil")
	}
	if _, ok := schema["properties"]; !ok {
		t.Errorf("Schema is missing JSON Schema properties field; got %v", schema)
	}
}

func TestPreferStableOrderRule_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg, ok := NewPreferStableOrderRule().DefaultConfig().(PreferStableOrderConfig)
	if !ok {
		t.Fatalf("DefaultConfig type = %T, want PreferStableOrderConfig", NewPreferStableOrderRule().DefaultConfig())
	}
	if cfg.Order == nil || *cfg.Order != string(preferStableOrderOCILogical) {
		t.Errorf("DefaultConfig.Order = %v, want %q", cfg.Order, preferStableOrderOCILogical)
	}
	if cfg.SortUnknown == nil || *cfg.SortUnknown != false {
		t.Errorf("DefaultConfig.SortUnknown = %v, want false", cfg.SortUnknown)
	}
}

func TestPreferStableOrderRule_ValidateConfig(t *testing.T) {
	t.Parallel()

	rule := NewPreferStableOrderRule()

	if err := rule.ValidateConfig(map[string]any{"order": "oci-logical"}); err != nil {
		t.Errorf("oci-logical should be valid: %v", err)
	}
	if err := rule.ValidateConfig(map[string]any{"order": "lexical"}); err != nil {
		t.Errorf("lexical should be valid: %v", err)
	}
	if err := rule.ValidateConfig(map[string]any{"order": "unknown"}); err == nil {
		t.Errorf("unknown enum should be rejected")
	}
	if err := rule.ValidateConfig(map[string]any{"order": 123}); err == nil {
		t.Errorf("non-string order should be rejected")
	}
	if err := rule.ValidateConfig(map[string]any{"sort-unknown": true}); err != nil {
		t.Errorf("sort-unknown=true should be valid: %v", err)
	}
	if err := rule.ValidateConfig(map[string]any{"sort-unknown": "yes"}); err == nil {
		t.Errorf("non-bool sort-unknown should be rejected")
	}
}

func TestPreferStableOrderRule_CheckHandlesNilFacts(t *testing.T) {
	t.Parallel()

	if got := NewPreferStableOrderRule().Check(rules.LintInput{}); got != nil {
		t.Errorf("Check with nil Facts returned %v, want nil", got)
	}
}

func TestPreferStableOrderRule_Check(t *testing.T) {
	t.Parallel()

	defaultCfg := DefaultPreferStableOrderConfig()
	lexCfg := DefaultPreferStableOrderConfig()
	lexOrder := string(preferStableOrderLexical)
	lexCfg.Order = &lexOrder
	sortUnknownCfg := DefaultPreferStableOrderConfig()
	sortUnknown := true
	sortUnknownCfg.SortUnknown = &sortUnknown

	testutil.RunRuleTests(t, NewPreferStableOrderRule(), []testutil.RuleTestCase{
		{
			Name: "already in OCI logical order is clean",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.description="desc" \
      org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "two pairs swapped triggers a violation",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.description="desc" \
      org.opencontainers.image.title="demo"
`,
			WantViolations: 1,
			WantMessages:   []string{"not in the configured stable order"},
		},
		{
			Name: "three pairs out of OCI order",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.description="desc" \
      org.opencontainers.image.source="https://example.com" \
      org.opencontainers.image.title="demo"
`,
			WantViolations: 1,
		},
		{
			Name: "lexical mode flags reverse-sorted block",
			Content: `FROM alpine:3.20
LABEL b="2" \
      a="1"
`,
			Config:         lexCfg,
			WantViolations: 1,
		},
		{
			Name: "lexical mode is silent on lex-sorted unqualified keys",
			Content: `FROM alpine:3.20
LABEL a="1" \
      b="2"
`,
			Config:         lexCfg,
			WantViolations: 0,
		},
		{
			Name: "OCI mode flags out-of-group ordering even when lex-sorted",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.description="desc" \
      org.opencontainers.image.title="demo"
`,
			Config:         defaultCfg,
			WantViolations: 1,
		},
		{
			Name: "label-schema after OCI is wrong; OCI moves first",
			Content: `FROM alpine:3.20
LABEL org.label-schema.name="legacy" \
      org.opencontainers.image.title="demo"
`,
			WantViolations: 1,
		},
		{
			Name: "io.k8s catalog after OCI is wrong; OCI moves first",
			Content: `FROM alpine:3.20
LABEL io.k8s.display-name="demo" \
      org.opencontainers.image.title="demo"
`,
			WantViolations: 1,
		},
		{
			Name: "all-unknown reverse-DNS keys with sort-unknown=false is clean",
			Content: `FROM alpine:3.20
LABEL com.example.zeta="z" \
      com.example.alpha="a"
`,
			WantViolations: 0,
		},
		{
			Name: "all-unknown reverse-DNS keys with sort-unknown=true is flagged",
			Content: `FROM alpine:3.20
LABEL com.example.zeta="z" \
      com.example.alpha="a"
`,
			Config:         sortUnknownCfg,
			WantViolations: 1,
		},
		{
			Name: "single-line multi-pair LABEL out of order: report only, no fix",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" org.opencontainers.image.title="t"
`,
			WantViolations: 1,
		},
		{
			Name: "single multi-pair LABEL with content untouched is clean (no fix)",
			Content: `FROM alpine:3.20
# Comments above LABEL are fine.
LABEL org.opencontainers.image.title="t" \
      org.opencontainers.image.description="d"
`,
			WantViolations: 0,
		},
		{
			Name: "duplicate key in same stage suppresses our rule (defer to no-duplicate-keys)",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" \
      org.opencontainers.image.title="first" \
      org.opencontainers.image.title="second"
`,
			WantViolations: 0,
		},
		{
			Name: "dynamic key in block suppresses the rule",
			Content: `FROM alpine:3.20
ARG LABEL_PREFIX=com.example
LABEL "$LABEL_PREFIX.name"="dynamic" \
      org.opencontainers.image.title="demo"
`,
			WantViolations: 0,
		},
		{
			Name: "single-pair LABEL is never flagged",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="solo"
`,
			WantViolations: 0,
		},
		{
			Name: "two stages both out of order yield two violations",
			Content: `FROM alpine:3.20 AS a
LABEL org.opencontainers.image.description="a" \
      org.opencontainers.image.title="a"

FROM alpine:3.20
LABEL org.opencontainers.image.description="b" \
      org.opencontainers.image.title="b"
`,
			WantViolations: 2,
		},
	})
}

func TestPreferStableOrderRule_FixSwapsTwoPairs(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.description="desc" \
      org.opencontainers.image.title="demo"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	fix := violations[0].PreferredFix()
	if fix == nil {
		t.Fatal("expected preferred fix")
	}
	if fix.Safety != rules.FixSafe {
		t.Errorf("fix safety = %s, want safe", fix.Safety)
	}
	if fix.Priority != 95 {
		t.Errorf("fix priority = %d, want 95", fix.Priority)
	}
	got := string(fixpkg.ApplyFix([]byte(content), fix))
	want := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.description="desc"
`
	if got != want {
		t.Errorf("fix mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferStableOrderRule_FixThreeWayPermutation(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" \
      org.opencontainers.image.source="https://example.com" \
      org.opencontainers.image.title="t"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	got := string(fixpkg.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := `FROM alpine:3.20
LABEL org.opencontainers.image.title="t" \
      org.opencontainers.image.description="d" \
      org.opencontainers.image.source="https://example.com"
`
	if got != want {
		t.Errorf("fix mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferStableOrderRule_FixHonorsBacktickEscape(t *testing.T) {
	t.Parallel()

	content := "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"LABEL org.opencontainers.image.description=\"d\" `\n" +
		"      org.opencontainers.image.title=\"t\"\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1; messages: %v", len(violations), violations)
	}
	got := string(fixpkg.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"LABEL org.opencontainers.image.title=\"t\" `\n" +
		"      org.opencontainers.image.description=\"d\"\n"
	if got != want {
		t.Errorf("fix mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferStableOrderRule_NoFixForSingleLineMultiPair(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" org.opencontainers.image.title="t"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if got := violations[0].PreferredFix(); got != nil {
		t.Errorf("expected no fix for single-line multi-pair LABEL, got %+v", got)
	}
}

func TestPreferStableOrderRule_DetailMessage(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" \
      org.opencontainers.image.title="t"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if !strings.Contains(violations[0].Detail, "OCI logical groups") {
		t.Errorf("expected OCI-logical detail, got %q", violations[0].Detail)
	}
}

func TestKeyRank_OCIGroups(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key       string
		groupRank int
		keyRank   int
	}{
		{ocispec.AnnotationTitle, 1, 1},
		{ocispec.AnnotationDescription, 1, 2},
		{ocispec.AnnotationSource, 2, 1},
		{ocispec.AnnotationURL, 2, 2},
		{ocispec.AnnotationDocumentation, 2, 3},
		{ocispec.AnnotationAuthors, 3, 1},
		{ocispec.AnnotationVendor, 3, 2},
		{ocispec.AnnotationLicenses, 3, 3},
		{ocispec.AnnotationVersion, 4, 1},
		{ocispec.AnnotationRevision, 4, 2},
		{ocispec.AnnotationCreated, 4, 3},
		{ocispec.AnnotationRefName, 4, 4},
		{ocispec.AnnotationBaseImageName, 5, 1},
		{ocispec.AnnotationBaseImageDigest, 5, 2},
		{ioK8sDisplayName, 6, 1},
		{ioK8sDescription, 6, 2},
		{ioOpenShiftTags, 6, 3},
		{ioOpenShiftExposeServices, 6, 4},
		{ioOpenShiftS2IScriptsURL, 6, 5},
		{dockerImageSourceEntrypoint, 7, 1},
		{maintainerKey, 8, 100},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			t.Parallel()
			got := keyRank(c.key, 0, preferStableOrderOCILogical, false)
			if got.groupRank != c.groupRank || got.keyRank != c.keyRank {
				t.Errorf("keyRank(%q) = (group=%d, key=%d), want (group=%d, key=%d)",
					c.key, got.groupRank, got.keyRank, c.groupRank, c.keyRank)
			}
		})
	}
}

func TestKeyRank_DockerExtension(t *testing.T) {
	t.Parallel()

	r := keyRank("com.docker.extension.icon", 0, preferStableOrderOCILogical, false)
	if r.groupRank != dockerEcosystemGroupRank {
		t.Errorf("docker extension key groupRank = %d, want %d", r.groupRank, dockerEcosystemGroupRank)
	}
}

func TestKeyRank_LabelSchema(t *testing.T) {
	t.Parallel()

	a := keyRank("org.label-schema.name", 0, preferStableOrderOCILogical, false)
	b := keyRank("org.label-schema.version", 1, preferStableOrderOCILogical, false)
	if a.groupRank != legacyGroupRank || b.groupRank != legacyGroupRank {
		t.Fatalf("label-schema groupRank = %d/%d, want %d", a.groupRank, b.groupRank, legacyGroupRank)
	}
	if a.namespaceRank == "" || b.namespaceRank == "" {
		t.Fatalf("label-schema namespaceRank is empty: a=%q, b=%q", a.namespaceRank, b.namespaceRank)
	}
	if cmpOrderRank(a, b) >= 0 {
		t.Errorf("label-schema.name should sort before label-schema.version (lex by suffix)")
	}
}

func TestKeyRank_UnknownReverseDNS(t *testing.T) {
	t.Parallel()

	a := keyRank("com.example.foo", 0, preferStableOrderOCILogical, true)
	b := keyRank("com.example.bar", 1, preferStableOrderOCILogical, true)
	if a.groupRank != unknownReverseDNSGroupRank {
		t.Errorf("unknown reverse-DNS groupRank = %d, want %d", a.groupRank, unknownReverseDNSGroupRank)
	}
	if a.namespaceRank != "com.example" || b.namespaceRank != "com.example" {
		t.Errorf("unknown reverse-DNS namespaceRank: a=%q, b=%q (want com.example each)",
			a.namespaceRank, b.namespaceRank)
	}

	// sort-unknown=false → namespaceRank empty so order is preserved
	c := keyRank("com.example.foo", 0, preferStableOrderOCILogical, false)
	if c.namespaceRank != "" {
		t.Errorf("with sort-unknown=false, namespaceRank should be empty; got %q", c.namespaceRank)
	}
}

func TestKeyRank_UnqualifiedUnknown(t *testing.T) {
	t.Parallel()

	r := keyRank("flavor", 0, preferStableOrderOCILogical, false)
	if r.groupRank != unknownUnqualifiedGroupRank {
		t.Errorf("unqualified unknown groupRank = %d, want %d", r.groupRank, unknownUnqualifiedGroupRank)
	}
}

func TestKeyRank_Lexical(t *testing.T) {
	t.Parallel()

	a := keyRank("zeta", 0, preferStableOrderLexical, false)
	b := keyRank("alpha", 1, preferStableOrderLexical, false)
	if a.groupRank != 0 || b.groupRank != 0 {
		t.Errorf("lexical mode groupRank should be 0; got %d, %d", a.groupRank, b.groupRank)
	}
	if cmpOrderRank(a, b) <= 0 {
		t.Errorf("lexical: zeta should sort after alpha")
	}
}

func TestStableSortPermutation(t *testing.T) {
	t.Parallel()

	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		ranks := []orderRank{
			{groupRank: 1, originalIndex: 0},
			{groupRank: 2, originalIndex: 1},
			{groupRank: 3, originalIndex: 2},
		}
		got := stableSortPermutation(ranks)
		if !isIdentityPermutation(got) {
			t.Errorf("identity sort = %v, want [0,1,2]", got)
		}
	})

	t.Run("full reverse", func(t *testing.T) {
		t.Parallel()
		ranks := []orderRank{
			{groupRank: 3, originalIndex: 0},
			{groupRank: 2, originalIndex: 1},
			{groupRank: 1, originalIndex: 2},
		}
		got := stableSortPermutation(ranks)
		want := []int{2, 1, 0}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("reverse sort = %v, want %v", got, want)
				break
			}
		}
	})

	t.Run("equal ranks preserve original order", func(t *testing.T) {
		t.Parallel()
		ranks := []orderRank{
			{groupRank: 1, originalIndex: 0},
			{groupRank: 1, originalIndex: 1},
			{groupRank: 1, originalIndex: 2},
		}
		got := stableSortPermutation(ranks)
		if !isIdentityPermutation(got) {
			t.Errorf("equal-rank stability broken: %v", got)
		}
	})
}

func TestIsUnknownReverseDNS(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key  string
		want bool
	}{
		{"com.example.foo", true},
		{"io.test", true},
		{"a-b.c", true},
		{"flavor", false},
		{"", false},
		{".foo", false},
		{"COM.EXAMPLE", false},
		{"com_example.foo", false},
	}
	for _, c := range cases {
		if got := isUnknownReverseDNS(c.key); got != c.want {
			t.Errorf("isUnknownReverseDNS(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestUnknownNamespace(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"com.example.foo":     "com.example",
		"com.example.foo.bar": "com.example",
		"single":              "single",
		"a.b":                 "a.b",
	}
	for in, want := range cases {
		if got := unknownNamespace(in); got != want {
			t.Errorf("unknownNamespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPairsAreOnSeparateLines(t *testing.T) {
	t.Parallel()

	multiLine := []labelWordSpan{
		{start: sourcePosition{line: 2, col: 6}},
		{start: sourcePosition{line: 3, col: 6}},
	}
	if !pairsAreOnSeparateLines(multiLine) {
		t.Error("multi-line pairs should be separate")
	}

	singleLine := []labelWordSpan{
		{start: sourcePosition{line: 2, col: 6}},
		{start: sourcePosition{line: 2, col: 30}},
	}
	if pairsAreOnSeparateLines(singleLine) {
		t.Error("single-line multi-pair should report not-separate")
	}
}

func TestBuildStableOrderFix_DefensiveBranches(t *testing.T) {
	t.Parallel()

	meta := NewPreferStableOrderRule().Metadata()
	if got := buildStableOrderFix("Dockerfile", nil, facts.LabelInstructionFact{}, []int{0, 1}, '\\', meta); got != nil {
		t.Error("expected nil fix when SourceMap is nil")
	}
}

func TestInterSpanGapsAreClean_NilSourceMap(t *testing.T) {
	t.Parallel()
	if interSpanGapsAreClean(nil, nil, '\\') {
		t.Error("nil source map should report not clean")
	}
}

func TestInterSpanGapsAreClean_RejectsCommentLine(t *testing.T) {
	t.Parallel()

	// A LABEL with a comment between continuation lines is not safely
	// reorderable. Verify the rule reports but does not emit an auto-fix.
	content := `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" \
      # keep this comment
      org.opencontainers.image.title="t"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected one violation, got %d", len(violations))
	}
	if got := violations[0].PreferredFix(); got != nil {
		t.Fatalf("expected no fix when comment splits LABEL pairs, got %+v", got)
	}
}

func TestPreferStableOrderRule_FixIsIdempotent(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.description="d" \
      org.opencontainers.image.title="t"
`
	rule := NewPreferStableOrderRule()
	violations := rule.Check(testutil.MakeLintInput(t, "Dockerfile", content))
	if len(violations) != 1 || violations[0].PreferredFix() == nil {
		t.Fatalf("expected one violation with fix, got %d", len(violations))
	}
	fixed := string(fixpkg.ApplyFix([]byte(content), violations[0].PreferredFix()))
	if again := rule.Check(testutil.MakeLintInput(t, "Dockerfile", fixed)); len(again) != 0 {
		t.Errorf("expected re-lint of fixed content to be clean, got %d violations", len(again))
	}
}

func TestPreferStableOrderRule_LabelSchemaSubLex(t *testing.T) {
	t.Parallel()

	// org.label-schema.* keys should sort lexically by suffix within the
	// legacy group. version > name lex; rule should flag the swap.
	content := `FROM alpine:3.20
LABEL org.label-schema.version="1" \
      org.label-schema.name="x"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferStableOrderRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1 (label-schema sub-lex)", len(violations))
	}
}
