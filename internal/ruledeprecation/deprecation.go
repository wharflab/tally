package ruledeprecation

import (
	"fmt"
	"slices"
	"strings"
)

// Kind describes how a deprecated rule should behave.
type Kind string

const (
	// KindDeadEnd marks a rule name that should no longer be used and has no replacement.
	KindDeadEnd Kind = "dead-end"

	// KindSuperseded marks a rule name that is kept as a compatibility alias for another rule.
	KindSuperseded Kind = "superseded"
)

// Entry describes one deprecated rule code.
type Entry struct {
	// Code is the canonical deprecated rule code.
	Code string

	// Aliases are additional spellings that should produce the same deprecation behavior.
	Aliases []string

	// Kind controls warning text and compatibility behavior.
	Kind Kind

	// Replacement is the rule code that supersedes Code. Only meaningful for KindSuperseded.
	Replacement string

	// RemoveIn optionally names the version where the deprecated spelling may be removed.
	RemoveIn string

	// Detail optionally explains why the rule was deprecated.
	Detail string
}

// Notice is a user-facing deprecation observation.
type Notice struct {
	Entry Entry
}

// Message returns the warning message without the "Warning: " prefix.
func (n Notice) Message() string {
	entry := n.Entry
	switch entry.Kind {
	case KindSuperseded:
		msg := fmt.Sprintf("rule %s is deprecated; use %s instead", entry.Code, entry.Replacement)
		if entry.Detail != "" {
			msg += ": " + entry.Detail
		}
		return msg
	case KindDeadEnd:
		msg := fmt.Sprintf("rule %s is deprecated; remove this rule setting or inline directive", entry.Code)
		if entry.RemoveIn != "" {
			msg += "; it may be removed in " + entry.RemoveIn
		}
		if entry.Detail != "" {
			msg += ": " + entry.Detail
		}
		return msg
	default:
		return fmt.Sprintf("rule %s is deprecated", entry.Code)
	}
}

// Key returns the stable deduplication key for this notice.
func (n Notice) Key() string {
	return n.Entry.Code
}

var entries = []Entry{
	// Keep BuildKit supersessions aligned with internal/rules/hadolint-status.json
	// entries whose status is "covered_by_buildkit"; tests enforce this.
	supersededByBuildKit("DL3000", "WorkdirRelativePath", "relative WORKDIR paths"),
	supersededByBuildKit("DL3012", "MultipleInstructionsDisallowed", "duplicate HEALTHCHECK instructions"),
	supersededByBuildKit("DL3024", "DuplicateStageName", "duplicate stage names"),
	supersededByBuildKit("DL3025", "JSONArgsRecommended", "shell-form CMD and ENTRYPOINT instructions"),
	supersededByBuildKit("DL3029", "FromPlatformFlagConstDisallowed", "constant FROM --platform values"),
	supersededByBuildKit("DL3044", "UndefinedVar", "undefined variable references"),
	supersededByBuildKit("DL3063", "ReservedStageName", "reserved stage names"),
	supersededByBuildKit("DL4000", "MaintainerDeprecated", "deprecated MAINTAINER instructions"),
	supersededByBuildKit("DL4003", "MultipleInstructionsDisallowed", "duplicate CMD instructions"),
	supersededByBuildKit("DL4004", "MultipleInstructionsDisallowed", "duplicate ENTRYPOINT instructions"),
}

var lookupByCode = buildLookup(entries)

func supersededByBuildKit(code, ruleName, subject string) Entry {
	return Entry{
		Code:        "hadolint/" + code,
		Aliases:     []string{code},
		Kind:        KindSuperseded,
		Replacement: "buildkit/" + ruleName,
		Detail:      "tally uses the BuildKit implementation for " + subject,
	}
}

func buildLookup(in []Entry) map[string]Entry {
	out := make(map[string]Entry, len(in)*2)
	for _, entry := range in {
		out[entry.Code] = entry
		for _, alias := range entry.Aliases {
			out[alias] = entry
		}
	}
	return out
}

// Lookup returns the deprecation entry for code or one of its aliases.
func Lookup(code string) (Entry, bool) {
	entry, ok := lookupByCode[strings.TrimSpace(code)]
	return entry, ok
}

// IsKnown reports whether code is a deprecated code or alias.
func IsKnown(code string) bool {
	_, ok := Lookup(code)
	return ok
}

// ReplacementFor returns the replacement rule for a superseded deprecated code.
func ReplacementFor(code string) (string, bool) {
	entry, ok := Lookup(code)
	if !ok || entry.Kind != KindSuperseded || entry.Replacement == "" {
		return "", false
	}
	return entry.Replacement, true
}

// DeprecatedCodesFor returns deprecated codes and aliases that target ruleCode.
func DeprecatedCodesFor(ruleCode string) []string {
	var out []string
	for _, entry := range entries {
		if entry.Kind != KindSuperseded || entry.Replacement != ruleCode {
			continue
		}
		out = append(out, entry.Code)
		out = append(out, entry.Aliases...)
	}
	return out
}

// IsDeprecatedAliasFor reports whether deprecatedCode is a superseded alias for ruleCode.
func IsDeprecatedAliasFor(deprecatedCode, ruleCode string) bool {
	replacement, ok := ReplacementFor(deprecatedCode)
	return ok && replacement == ruleCode
}

// Collector stores deprecation notices and deduplicates them by canonical deprecated code.
type Collector struct {
	notices map[string]Notice
}

// NewCollector creates an empty deprecation notice collector.
func NewCollector() *Collector {
	return &Collector{notices: make(map[string]Notice)}
}

// AddCode records a deprecation notice if code is deprecated.
func (c *Collector) AddCode(code string) {
	if c == nil {
		return
	}
	entry, ok := Lookup(code)
	if !ok {
		return
	}
	c.AddNotice(Notice{Entry: entry})
}

// AddNotice records a deprecation notice.
func (c *Collector) AddNotice(notice Notice) {
	if c == nil {
		return
	}
	if c.notices == nil {
		c.notices = make(map[string]Notice)
	}
	c.notices[notice.Key()] = notice
}

// AddNotices records multiple deprecation notices.
func (c *Collector) AddNotices(notices []Notice) {
	for _, notice := range notices {
		c.AddNotice(notice)
	}
}

// Notices returns deduplicated notices in stable order.
func (c *Collector) Notices() []Notice {
	if c == nil || len(c.notices) == 0 {
		return nil
	}
	out := make([]Notice, 0, len(c.notices))
	for _, notice := range c.notices {
		out = append(out, notice)
	}
	slices.SortFunc(out, func(a, b Notice) int {
		return strings.Compare(a.Entry.Code, b.Entry.Code)
	})
	return out
}
