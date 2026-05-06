package labels

import (
	"fmt"
	"strings"

	"github.com/distribution/reference"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NoStaleBaseDigestRuleCode is the full rule code.
const NoStaleBaseDigestRuleCode = rules.TallyRulePrefix + "labels/no-stale-base-digest"

// NoStaleBaseDigestRule flags base digest labels that cannot be tied to a digest-pinned FROM.
type NoStaleBaseDigestRule struct{}

type exportedBaseDigest struct {
	Digest    string
	HasDigest bool
}

// NewNoStaleBaseDigestRule creates a new rule instance.
func NewNoStaleBaseDigestRule() *NoStaleBaseDigestRule {
	return &NoStaleBaseDigestRule{}
}

// Metadata returns the rule metadata.
func (r *NoStaleBaseDigestRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoStaleBaseDigestRuleCode,
		Name:            "No stale base digest label",
		Description:     "Detects OCI base digest labels that are not backed by a digest-pinned FROM",
		DocURL:          rules.TallyDocURL(NoStaleBaseDigestRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the rule.
func (r *NoStaleBaseDigestRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	base := exportedBaseImageDigest(input)
	exportedStages := exportedImageStageIndexes(input)
	meta := r.Metadata()
	sm := input.SourceMap()
	escapeToken := labelEscapeToken(input)
	labels := input.Facts.Labels()
	activePairs := activeExportedLabelPairIDs(input)
	violations := make([]rules.Violation, 0, len(labels))
	for _, pair := range labels {
		if !shouldCheckBaseDigestLabelPair(pair, exportedStages, activePairs) {
			continue
		}
		message := baseDigestViolationMessage(pair, base)
		if message == "" {
			continue
		}

		violation := rules.NewViolation(
			rules.NewLocationFromRanges(input.File, pair.Location),
			meta.Code,
			message,
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL).WithDetail(
			"org.opencontainers.image.base.digest identifies the base image manifest digest, not the final image digest. " +
				"Keep it only when the exported image's external base is digest-pinned and the label mirrors that digest.",
		)
		if fixes := buildBaseDigestFixes(input.File, sm, pair, meta, escapeToken); len(fixes) > 0 {
			violation = violation.WithSuggestedFixes(fixes)
		}
		violations = append(violations, violation)
	}
	return violations
}

func shouldCheckBaseDigestLabelPair(
	pair facts.LabelPairFact,
	exportedStages map[int]bool,
	activePairs map[labelPairID]bool,
) bool {
	if pair.NoDelim || pair.KeyIsDynamic || pair.Key == "" {
		return false
	}
	if exportedStages != nil && !exportedStages[pair.StageIndex] {
		return false
	}
	if activePairs != nil && !activePairs[labelPairKey(pair)] {
		return false
	}
	return pair.Key == ocispec.AnnotationBaseImageDigest
}

func baseDigestViolationMessage(pair facts.LabelPairFact, base exportedBaseDigest) string {
	key := ocispec.AnnotationBaseImageDigest
	if !base.HasDigest {
		return fmt.Sprintf("label %q requires a digest-pinned FROM in the exported image's stage chain", key)
	}
	if pair.ValueIsDynamic {
		return ""
	}
	if pair.Value != base.Digest {
		return fmt.Sprintf("label %q is %q, but the exported base FROM is pinned to %q", key, pair.Value, base.Digest)
	}
	return ""
}

func exportedBaseImageDigest(input rules.LintInput) exportedBaseDigest {
	finalStage := input.FinalStageIndex()
	if finalStage < 0 {
		return exportedBaseDigest{}
	}
	if input.Semantic == nil {
		if finalStage >= len(input.Stages) {
			return exportedBaseDigest{}
		}
		raw := input.Stages[finalStage].BaseName
		digest, ok := imageRefDigest(raw)
		return exportedBaseDigest{Digest: digest, HasDigest: ok}
	}

	visited := map[int]bool{}
	for current := finalStage; current >= 0; {
		if visited[current] {
			return exportedBaseDigest{}
		}
		visited[current] = true
		info := input.Semantic.StageInfo(current)
		if info == nil || info.BaseImage == nil {
			return exportedBaseDigest{}
		}
		if info.BaseImage.IsStageRef {
			current = info.BaseImage.StageIndex
			continue
		}

		raw := info.BaseImage.Raw
		digest, ok := imageRefDigest(raw)
		return exportedBaseDigest{Digest: digest, HasDigest: ok}
	}
	return exportedBaseDigest{}
}

func imageRefDigest(raw string) (string, bool) {
	if strings.EqualFold(raw, "scratch") {
		return "", false
	}
	named, err := reference.ParseNormalizedNamed(raw)
	if err != nil {
		return "", false
	}
	digested, ok := named.(reference.Digested)
	if !ok {
		return "", false
	}
	return digested.Digest().String(), true
}

func buildBaseDigestFixes(
	file string,
	sm *sourcemap.SourceMap,
	pair facts.LabelPairFact,
	meta rules.RuleMetadata,
	escapeToken rune,
) []*rules.SuggestedFix {
	key := ocispec.AnnotationBaseImageDigest
	return buildLabelPairRemovalFixes(file, sm, pair, escapeToken, labelInstructionFixOptions{
		CommentDescription: fmt.Sprintf("Comment out LABEL %q without a matching digest-pinned FROM", key),
		DeleteDescription:  fmt.Sprintf("Delete LABEL %q without a matching digest-pinned FROM", key),
		CommentPrefix:      fmt.Sprintf("# [commented out by tally - %s needs a digest-pinned FROM]: ", key),
		Safety:             rules.FixSuggestion,
		Priority:           meta.FixPriority,
	})
}

func init() {
	rules.Register(NewNoStaleBaseDigestRule())
}
