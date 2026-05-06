package labels

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NoBuildxGitOverlapRuleCode is the full rule code.
const NoBuildxGitOverlapRuleCode = rules.TallyRulePrefix + "labels/no-buildx-git-overlap"

type buildxGitLabelsMode string

const (
	buildxGitLabelsOff  buildxGitLabelsMode = "off"
	buildxGitLabelsTrue buildxGitLabelsMode = "true"
	buildxGitLabelsFull buildxGitLabelsMode = "full"
)

// NoBuildxGitOverlapConfig configures how Buildx git label generation is detected.
type NoBuildxGitOverlapConfig struct {
	BuildxGitLabels string `json:"buildx-git-labels,omitempty" koanf:"buildx-git-labels"`
}

// DefaultNoBuildxGitOverlapConfig returns the default configuration.
func DefaultNoBuildxGitOverlapConfig() NoBuildxGitOverlapConfig {
	return NoBuildxGitOverlapConfig{BuildxGitLabels: string(buildxGitLabelsFull)}
}

// NoBuildxGitOverlapRule flags Dockerfile LABEL keys that Buildx can generate.
type NoBuildxGitOverlapRule struct {
	schema map[string]any
}

type buildxGitOverlapGroup struct {
	pairs []facts.LabelPairFact
	keys  []string
}

// NewNoBuildxGitOverlapRule creates a new rule instance.
func NewNoBuildxGitOverlapRule() *NoBuildxGitOverlapRule {
	schema, err := configutil.RuleSchema(NoBuildxGitOverlapRuleCode)
	if err != nil {
		panic(err)
	}
	return &NoBuildxGitOverlapRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *NoBuildxGitOverlapRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoBuildxGitOverlapRuleCode,
		Name:            "No Buildx git label overlap",
		Description:     "Detects Dockerfile LABEL keys that Buildx can generate from git metadata",
		DocURL:          rules.TallyDocURL(NoBuildxGitOverlapRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *NoBuildxGitOverlapRule) Schema() map[string]any { return r.schema }

// DefaultConfig returns the default configuration.
func (r *NoBuildxGitOverlapRule) DefaultConfig() any {
	return DefaultNoBuildxGitOverlapConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NoBuildxGitOverlapRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(NoBuildxGitOverlapRuleCode, config)
}

// Check runs the rule.
func (r *NoBuildxGitOverlapRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	cfg := r.resolveConfig(input.Config)
	mode := configuredBuildxGitLabelsMode(cfg)
	if mode == buildxGitLabelsOff {
		return nil
	}

	generated := buildxGeneratedLabelKeys(mode)
	if len(generated) == 0 {
		return nil
	}

	exportedStages := exportedImageStageIndexes(input)
	meta := r.Metadata()
	sm := input.SourceMap()
	escapeToken := labelEscapeToken(input)
	labels := input.Facts.Labels()
	activeKeys := activeExportedLabelCommandKeys(input)
	groups := makeBuildxGitOverlapGroups(labels, generated, exportedStages, activeKeys)
	violations := make([]rules.Violation, 0, len(groups))
	for _, group := range groups {
		violation := buildxGitOverlapViolation(input.File, meta, group, mode)
		if fixes := buildBuildxGitOverlapFixes(input, sm, group, meta, escapeToken); len(fixes) > 0 {
			violation = violation.WithSuggestedFixes(fixes)
		}
		violations = append(violations, violation)
	}
	return violations
}

func makeBuildxGitOverlapGroups(
	labels []facts.LabelPairFact,
	generated map[string]struct{},
	exportedStages map[int]bool,
	activeKeys map[labelCommandKey]bool,
) []buildxGitOverlapGroup {
	groupIndex := make(map[[2]int]int)
	groups := make([]buildxGitOverlapGroup, 0, len(labels))
	for _, pair := range labels {
		if !shouldCheckBuildxLabelPair(pair, exportedStages) {
			continue
		}
		if _, ok := generated[pair.Key]; !ok {
			continue
		}
		if activeKeys != nil && !activeKeys[labelCommandKey{
			stageIndex:   pair.StageIndex,
			commandIndex: pair.CommandIndex,
			key:          pair.Key,
		}] {
			continue
		}

		key := [2]int{pair.StageIndex, pair.CommandIndex}
		idx, ok := groupIndex[key]
		if !ok {
			groupIndex[key] = len(groups)
			groups = append(groups, buildxGitOverlapGroup{
				pairs: []facts.LabelPairFact{pair},
			})
			idx = len(groups) - 1
		} else {
			groups[idx].pairs = append(groups[idx].pairs, pair)
		}
		groups[idx].keys = appendUniqueBuildxLabelKey(groups[idx].keys, pair.Key)
	}
	return groups
}

func appendUniqueBuildxLabelKey(keys []string, key string) []string {
	if slices.Contains(keys, key) {
		return keys
	}
	return append(keys, key)
}

func (r *NoBuildxGitOverlapRule) resolveConfig(config any) NoBuildxGitOverlapConfig {
	return configutil.Coerce(config, DefaultNoBuildxGitOverlapConfig())
}

func configuredBuildxGitLabelsMode(cfg NoBuildxGitOverlapConfig) buildxGitLabelsMode {
	return normalizeBuildxGitLabelsMode(cfg.BuildxGitLabels)
}

func normalizeBuildxGitLabelsMode(raw string) buildxGitLabelsMode {
	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "":
		return buildxGitLabelsFull
	case string(buildxGitLabelsOff), "none":
		return buildxGitLabelsOff
	case string(buildxGitLabelsFull):
		return buildxGitLabelsFull
	}

	enabled, err := strconv.ParseBool(trimmed)
	if err != nil {
		return buildxGitLabelsOff
	}
	if enabled {
		return buildxGitLabelsTrue
	}
	return buildxGitLabelsOff
}

func buildxGeneratedLabelKeys(mode buildxGitLabelsMode) map[string]struct{} {
	switch mode {
	case buildxGitLabelsTrue:
		return map[string]struct{}{
			ocispec.AnnotationRevision:      {},
			dockerfileSourceEntrypointLabel: {},
		}
	case buildxGitLabelsFull:
		return map[string]struct{}{
			ocispec.AnnotationRevision:      {},
			ocispec.AnnotationSource:        {},
			dockerfileSourceEntrypointLabel: {},
		}
	case buildxGitLabelsOff:
		return nil
	default:
		return nil
	}
}

func exportedImageStageIndexes(input rules.LintInput) map[int]bool {
	chain := exportedImageStageChain(input)
	if len(chain) == 0 {
		return nil
	}

	indexes := make(map[int]bool, len(chain))
	for _, stageIdx := range chain {
		indexes[stageIdx] = true
	}
	return indexes
}

func shouldCheckBuildxLabelPair(pair facts.LabelPairFact, exportedStages map[int]bool) bool {
	if pair.NoDelim || pair.KeyIsDynamic || pair.Key == "" {
		return false
	}
	return exportedStages == nil || exportedStages[pair.StageIndex]
}

func buildxGitOverlapViolation(
	file string,
	meta rules.RuleMetadata,
	group buildxGitOverlapGroup,
	mode buildxGitLabelsMode,
) rules.Violation {
	return rules.NewViolation(
		rules.NewLocationFromRanges(file, group.pairs[0].Location),
		meta.Code,
		fmt.Sprintf(
			"Buildx with BUILDX_GIT_LABELS=%s can emit %s; remove the Dockerfile label or disable the generated label source",
			buildxGitLabelsModeDisplay(mode),
			formatBuildxLabelKeys(group.keys),
		),
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"Generated git labels track the build input at invocation time. Keeping the same key in the Dockerfile can leave " +
			"stale source, revision, or Dockerfile-path metadata on the image.",
	)
}

func buildBuildxGitOverlapFixes(
	input rules.LintInput,
	sm *sourcemap.SourceMap,
	group buildxGitOverlapGroup,
	meta rules.RuleMetadata,
	escapeToken rune,
) []*rules.SuggestedFix {
	if len(group.keys) != 1 || group.keys[0] != ocispec.AnnotationRevision {
		return nil
	}
	key := ocispec.AnnotationRevision
	revisionPairs := make([]facts.LabelPairFact, 0, len(group.pairs))
	for _, pair := range group.pairs {
		if pair.Key != key {
			return nil
		}
		revisionPairs = append(revisionPairs, pair)
	}
	if candidates := removableBuildxRevisionPairs(input, key, group.pairs[0].StageIndex); len(candidates) > 0 {
		revisionPairs = candidates
	}
	return buildLabelPairsRemovalFixesAcrossCommands(input.File, sm, revisionPairs, escapeToken, labelInstructionFixOptions{
		CommentDescription: fmt.Sprintf("Comment out Dockerfile LABEL %q generated by Buildx", key),
		DeleteDescription:  fmt.Sprintf("Delete Dockerfile LABEL %q generated by Buildx", key),
		CommentPrefix:      fmt.Sprintf("# [commented out by tally - Buildx can generate %s]: ", key),
		Safety:             rules.FixSuggestion,
		Priority:           meta.FixPriority,
	})
}

func removableBuildxRevisionPairs(input rules.LintInput, key string, stageIndex int) []facts.LabelPairFact {
	candidates := exportedLabelPairsByKey(input, key)
	if len(candidates) == 0 {
		return nil
	}

	pairs := make([]facts.LabelPairFact, 0, len(candidates))
	for _, pair := range candidates {
		if pair.StageIndex != stageIndex || pair.NoDelim {
			continue
		}
		pairs = append(pairs, pair)
	}
	return pairs
}

func buildxGitLabelsModeDisplay(mode buildxGitLabelsMode) string {
	if mode == buildxGitLabelsTrue {
		return "1"
	}
	return string(mode)
}

func formatBuildxLabelKeys(keys []string) string {
	if len(keys) == 1 {
		return fmt.Sprintf("label %q", keys[0])
	}
	quoted := make([]string, 0, len(keys))
	for _, key := range keys {
		quoted = append(quoted, fmt.Sprintf("%q", key))
	}
	return "labels " + strings.Join(quoted, ", ")
}

func init() {
	rules.Register(NewNoBuildxGitOverlapRule())
}
