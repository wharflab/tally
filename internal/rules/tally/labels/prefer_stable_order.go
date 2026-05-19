package labels

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/sourcemap"
)

// PreferStableOrderRuleCode is the full rule code.
const PreferStableOrderRuleCode = rules.TallyRulePrefix + "labels/prefer-stable-order"

type preferStableOrderMode string

const (
	preferStableOrderOCILogical preferStableOrderMode = "oci-logical"
	preferStableOrderLexical    preferStableOrderMode = "lexical"
)

// PreferStableOrderConfig configures the prefer-stable-order rule.
type PreferStableOrderConfig struct {
	// Order selects the comparator. Either "oci-logical" or "lexical".
	Order *string `json:"order,omitempty" koanf:"order"`
	// SortUnknown decides whether unknown reverse-DNS keys are clustered by
	// namespace and sorted lexically within each namespace. When false, custom
	// keys keep their relative source order.
	SortUnknown *bool `json:"sort-unknown,omitempty" koanf:"sort-unknown"`
}

// DefaultPreferStableOrderConfig returns the default configuration.
func DefaultPreferStableOrderConfig() PreferStableOrderConfig {
	order := string(preferStableOrderOCILogical)
	sortUnknown := false
	return PreferStableOrderConfig{Order: &order, SortUnknown: &sortUnknown}
}

// PreferStableOrderRule reorders LABEL pairs into a deterministic order.
type PreferStableOrderRule struct {
	schema map[string]any
}

// NewPreferStableOrderRule creates a new rule instance.
func NewPreferStableOrderRule() *PreferStableOrderRule {
	schema, err := configutil.RuleSchema(PreferStableOrderRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferStableOrderRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferStableOrderRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferStableOrderRuleCode,
		Name:            "Prefer stable LABEL key order",
		Description:     "Reorder LABEL key/value pairs into a deterministic, human-readable order",
		DocURL:          rules.TallyDocURL(PreferStableOrderRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "style",
		IsExperimental:  false,
		// Priority 95: runs before prefer-grouped (96) and
		// newline-per-chained-call (97). Per-pair span edits stay inside the
		// LABEL instruction and don't overlap structural rewrites that operate
		// at instruction boundaries.
		FixPriority: 95,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferStableOrderRule) Schema() map[string]any { return r.schema }

// DefaultConfig returns the default configuration.
func (r *PreferStableOrderRule) DefaultConfig() any {
	return DefaultPreferStableOrderConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferStableOrderRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(PreferStableOrderRuleCode, config)
}

// Check runs the rule.
func (r *PreferStableOrderRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	cfg := r.resolveConfig(input.Config)
	mode := preferStableOrderMode(*cfg.Order)
	sortUnknown := *cfg.SortUnknown

	meta := r.Metadata()
	sm := input.SourceMap()
	escapeToken := labelEscapeToken(input)

	ctx := evaluateContext{
		file:        input.File,
		sm:          sm,
		mode:        mode,
		sortUnknown: sortUnknown,
		escapeToken: escapeToken,
		meta:        meta,
	}
	var violations []rules.Violation
	for _, stage := range input.Facts.Stages() {
		if stage == nil {
			continue
		}
		ctx.dupKeys = stageDuplicateKeySet(stage)
		for _, inst := range stage.LabelInstructions {
			if v, ok := r.evaluate(ctx, inst); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

func (r *PreferStableOrderRule) resolveConfig(config any) PreferStableOrderConfig {
	return configutil.Coerce(config, DefaultPreferStableOrderConfig())
}

// evaluateContext carries shared inputs through Check loops without exceeding
// the per-function argument-count lint limit.
type evaluateContext struct {
	file        string
	sm          *sourcemap.SourceMap
	dupKeys     map[string]struct{}
	mode        preferStableOrderMode
	sortUnknown bool
	escapeToken rune
	meta        rules.RuleMetadata
}

func (r *PreferStableOrderRule) evaluate(
	ctx evaluateContext,
	inst facts.LabelInstructionFact,
) (rules.Violation, bool) {
	if inst.Command == nil || len(inst.Pairs) < 2 {
		return rules.Violation{}, false
	}
	for _, p := range inst.Pairs {
		if p.KeyIsDynamic || p.NoDelim || p.ExpansionError != "" || p.Key == "" {
			return rules.Violation{}, false
		}
		if _, dup := ctx.dupKeys[p.Key]; dup {
			return rules.Violation{}, false
		}
	}

	ranks := make([]orderRank, len(inst.Pairs))
	for i, p := range inst.Pairs {
		ranks[i] = keyRank(p.Key, i, ctx.mode, ctx.sortUnknown)
	}

	if ctx.mode == preferStableOrderOCILogical && !ctx.sortUnknown {
		allUnknown := true
		for _, r := range ranks {
			if r.groupRank < unknownReverseDNSGroupRank {
				allUnknown = false
				break
			}
		}
		if allUnknown {
			return rules.Violation{}, false
		}
	}

	permutation := stableSortPermutation(ranks)
	if isIdentityPermutation(permutation) {
		return rules.Violation{}, false
	}

	loc := rules.NewLocationFromRanges(ctx.file, inst.Location)
	msg := "label keys in this LABEL block are not in the configured stable order"
	detail := stableOrderDetail(ctx.mode)
	v := rules.NewViolation(loc, ctx.meta.Code, msg, ctx.meta.DefaultSeverity).
		WithDocURL(ctx.meta.DocURL).
		WithDetail(detail)
	if fix := buildStableOrderFix(ctx.file, ctx.sm, inst, permutation, ctx.escapeToken, ctx.meta); fix != nil {
		v = v.WithSuggestedFix(fix)
	}
	return v, true
}

func stableOrderDetail(mode preferStableOrderMode) string {
	if mode == preferStableOrderLexical {
		return "Reorder LABEL pairs lexically. A stable key order keeps metadata diffs small and easy to review."
	}
	return strings.Join([]string{
		"Reorder LABEL pairs into the OCI logical groups: identity (title, description);",
		"source/refs (source, url, documentation); ownership/legal (authors, vendor, licenses);",
		"release/provenance (version, revision, created, ref.name); base image (base.name, base.digest);",
		"ecosystem and legacy keys last.",
		"A stable key order keeps metadata diffs small and easy to review.",
	}, " ")
}

func stageDuplicateKeySet(stage *facts.StageFacts) map[string]struct{} {
	groups := stage.DuplicateLabelGroups()
	if len(groups) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(groups))
	for key := range groups {
		out[key] = struct{}{}
	}
	return out
}

// orderRank is the deterministic comparator key for one label key.
// Lower is earlier. Compare by groupRank, then keyRank, then namespaceRank,
// then keyLex (lex within namespace when applicable), then originalIndex
// (which makes the comparator a stable sort).
type orderRank struct {
	groupRank     int
	keyRank       int
	namespaceRank string
	keyLex        string
	originalIndex int
}

const (
	identityGroupRank           = 1
	sourceRefsGroupRank         = 2
	ownershipGroupRank          = 3
	releaseGroupRank            = 4
	baseImageGroupRank          = 5
	openshiftCatalogGroupRank   = 6
	dockerEcosystemGroupRank    = 7
	legacyGroupRank             = 8
	unknownReverseDNSGroupRank  = 9
	unknownUnqualifiedGroupRank = 10
	dockerExtensionPrefix       = "com.docker.extension."
	dockerImageSourceEntrypoint = "com.docker.image.source.entrypoint"
	labelSchemaPrefix           = "org.label-schema."
	maintainerKey               = command.Maintainer
	ioK8sDisplayName            = "io.k8s.display-name"
	ioK8sDescription            = "io.k8s.description"
	ioOpenShiftTags             = "io.openshift.tags"
	ioOpenShiftExposeServices   = "io.openshift.expose-services"
	ioOpenShiftS2IScriptsURL    = "io.openshift.s2i.scripts-url"
)

// ociKnownKeyRanks maps a known label key to its (groupRank, keyRank).
// Built once at package init from OCI Go constants to avoid hard-coding the
// annotation key strings in rule code.
var ociKnownKeyRanks = func() map[string]struct {
	groupRank int
	keyRank   int
} {
	m := map[string]struct {
		groupRank int
		keyRank   int
	}{
		ocispec.AnnotationTitle:           {identityGroupRank, 1},
		ocispec.AnnotationDescription:     {identityGroupRank, 2},
		ocispec.AnnotationSource:          {sourceRefsGroupRank, 1},
		ocispec.AnnotationURL:             {sourceRefsGroupRank, 2},
		ocispec.AnnotationDocumentation:   {sourceRefsGroupRank, 3},
		ocispec.AnnotationAuthors:         {ownershipGroupRank, 1},
		ocispec.AnnotationVendor:          {ownershipGroupRank, 2},
		ocispec.AnnotationLicenses:        {ownershipGroupRank, 3},
		ocispec.AnnotationVersion:         {releaseGroupRank, 1},
		ocispec.AnnotationRevision:        {releaseGroupRank, 2},
		ocispec.AnnotationCreated:         {releaseGroupRank, 3},
		ocispec.AnnotationRefName:         {releaseGroupRank, 4},
		ocispec.AnnotationBaseImageName:   {baseImageGroupRank, 1},
		ocispec.AnnotationBaseImageDigest: {baseImageGroupRank, 2},
		ioK8sDisplayName:                  {openshiftCatalogGroupRank, 1},
		ioK8sDescription:                  {openshiftCatalogGroupRank, 2},
		ioOpenShiftTags:                   {openshiftCatalogGroupRank, 3},
		ioOpenShiftExposeServices:         {openshiftCatalogGroupRank, 4},
		ioOpenShiftS2IScriptsURL:          {openshiftCatalogGroupRank, 5},
		dockerImageSourceEntrypoint:       {dockerEcosystemGroupRank, 1},
		maintainerKey:                     {legacyGroupRank, 100},
	}
	return m
}()

// keyRank returns the orderRank for a key under the configured mode.
// originalIndex is the pair's source position (0-based PairIndex).
func keyRank(key string, originalIndex int, mode preferStableOrderMode, sortUnknown bool) orderRank {
	if mode == preferStableOrderLexical {
		return orderRank{
			groupRank:     0,
			keyRank:       0,
			namespaceRank: key,
			originalIndex: originalIndex,
		}
	}
	if rank, ok := ociKnownKeyRanks[key]; ok {
		return orderRank{
			groupRank:     rank.groupRank,
			keyRank:       rank.keyRank,
			namespaceRank: "",
			originalIndex: originalIndex,
		}
	}
	if strings.HasPrefix(key, dockerExtensionPrefix) {
		return orderRank{
			groupRank:     dockerEcosystemGroupRank,
			keyRank:       2,
			namespaceRank: key,
			originalIndex: originalIndex,
		}
	}
	if strings.HasPrefix(key, labelSchemaPrefix) {
		return orderRank{
			groupRank:     legacyGroupRank,
			keyRank:       1,
			namespaceRank: key[len(labelSchemaPrefix):],
			keyLex:        key,
			originalIndex: originalIndex,
		}
	}
	if isUnknownReverseDNS(key) {
		ns, lex := "", ""
		if sortUnknown {
			ns = unknownNamespace(key)
			lex = key
		}
		return orderRank{
			groupRank:     unknownReverseDNSGroupRank,
			keyRank:       0,
			namespaceRank: ns,
			keyLex:        lex,
			originalIndex: originalIndex,
		}
	}
	return orderRank{
		groupRank:     unknownUnqualifiedGroupRank,
		keyRank:       0,
		namespaceRank: "",
		originalIndex: originalIndex,
	}
}

// isUnknownReverseDNS reports whether the key looks like a reverse-DNS key
// (contains a dot and the leading segment is alphanumeric/hyphen). The caller
// should have already excluded keys that map to known group ranks.
func isUnknownReverseDNS(key string) bool {
	if key == "" {
		return false
	}
	dot := strings.IndexByte(key, '.')
	if dot <= 0 {
		return false
	}
	for _, r := range key[:dot] {
		if !isLowerAlphaNumOrHyphen(r) {
			return false
		}
	}
	return true
}

// unknownNamespace returns a sortable namespace for an unknown reverse-DNS
// key. It uses the first two dotted segments when available so that keys like
// `com.example.foo.bar` and `com.example.alpha.beta` cluster under
// `com.example`.
func unknownNamespace(key string) string {
	first := strings.IndexByte(key, '.')
	if first <= 0 {
		return key
	}
	rest := key[first+1:]
	second := strings.IndexByte(rest, '.')
	if second <= 0 {
		return key
	}
	return key[:first+1+second]
}

func isLowerAlphaNumOrHyphen(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}

// cmpOrderRank returns -1/0/+1 ordering a before b.
func cmpOrderRank(a, b orderRank) int {
	if c := cmp.Compare(a.groupRank, b.groupRank); c != 0 {
		return c
	}
	if c := cmp.Compare(a.keyRank, b.keyRank); c != 0 {
		return c
	}
	if c := cmp.Compare(a.namespaceRank, b.namespaceRank); c != 0 {
		return c
	}
	if c := cmp.Compare(a.keyLex, b.keyLex); c != 0 {
		return c
	}
	return cmp.Compare(a.originalIndex, b.originalIndex)
}

// stableSortPermutation returns indices `result` such that the desired order
// is `ranks[result[0]], ranks[result[1]], ...`. The originalIndex tiebreaker
// inside cmpOrderRank guarantees stability without requiring SortStableFunc.
func stableSortPermutation(ranks []orderRank) []int {
	idx := make([]int, len(ranks))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		return cmpOrderRank(ranks[a], ranks[b])
	})
	return idx
}

func isIdentityPermutation(perm []int) bool {
	for i, p := range perm {
		if i != p {
			return false
		}
	}
	return true
}

// buildStableOrderFix emits per-pair span swaps inside an already-multi-line
// LABEL. It returns nil when the LABEL is single-line multi-pair (defer to
// newline-per-chained-call), when comments split pairs, or when source spans
// can't be resolved.
func buildStableOrderFix(
	file string,
	sm *sourcemap.SourceMap,
	inst facts.LabelInstructionFact,
	permutation []int,
	escapeToken rune,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if sm == nil || inst.Command == nil {
		return nil
	}
	spans := labelPairSourceSpans(sm, inst.Command, escapeToken)
	if len(spans) != len(inst.Pairs) {
		return nil
	}
	if !pairsAreOnSeparateLines(spans) {
		return nil
	}
	if !interSpanGapsAreClean(sm, spans, escapeToken) {
		return nil
	}

	var edits []rules.TextEdit
	for newPos, oldPos := range permutation {
		if newPos == oldPos {
			continue
		}
		target := spans[newPos]
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(
				file,
				target.start.line, target.start.col,
				target.end.line, target.end.col,
			),
			NewText: spans[oldPos].text,
		})
	}
	if len(edits) == 0 {
		return nil
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Reorder %d LABEL keys into stable order", len(spans)),
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		IsPreferred: true,
		Edits:       edits,
	}
}

// pairsAreOnSeparateLines reports whether every pair span starts on a unique
// source line. When two pairs share a starting line, the LABEL is single-line
// multi-pair (or has multiple pairs on a continuation line); we let
// newline-per-chained-call split it first.
func pairsAreOnSeparateLines(spans []labelWordSpan) bool {
	seen := make(map[int]struct{}, len(spans))
	for _, s := range spans {
		if _, dup := seen[s.start.line]; dup {
			return false
		}
		seen[s.start.line] = struct{}{}
	}
	return true
}

// interSpanGapsAreClean checks the source text between consecutive pair spans.
// The only allowed content is optional spaces/tabs, the escape rune, and a
// single newline followed by leading whitespace on the next line. Comment
// lines or blank gap lines force the rule to skip the fix.
func interSpanGapsAreClean(sm *sourcemap.SourceMap, spans []labelWordSpan, escapeToken rune) bool {
	if sm == nil {
		return false
	}
	for i := range len(spans) - 1 {
		end := spans[i].end
		next := spans[i+1].start
		if next.line < end.line || next.line == end.line {
			return false
		}
		if next.line-end.line != 1 {
			return false
		}
		// Trailing portion of the line that ends the current span.
		endLine := sm.Line(end.line - 1)
		if end.col > len(endLine) {
			return false
		}
		trailing := strings.Trim(endLine[end.col:], " \t")
		if trailing != string(escapeToken) {
			return false
		}
		// Leading portion of the line that begins the next span.
		nextLine := sm.Line(next.line - 1)
		if next.col > len(nextLine) {
			return false
		}
		leading := nextLine[:next.col]
		for _, r := range leading {
			if r != ' ' && r != '\t' {
				return false
			}
		}
	}
	return true
}

func init() {
	rules.Register(NewPreferStableOrderRule())
}
