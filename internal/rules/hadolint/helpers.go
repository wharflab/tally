package hadolint

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
)

func topLevelInstructionNodes(root *parser.Node) []*parser.Node {
	if root == nil {
		return nil
	}

	nodes := make([]*parser.Node, 0, len(root.Children))
	for _, child := range root.Children {
		if child != nil {
			nodes = append(nodes, child)
		}
	}

	slices.SortFunc(nodes, func(a, b *parser.Node) int {
		return cmp.Compare(a.StartLine, b.StartLine)
	})

	return nodes
}

func onbuildTriggerKeyword(node *parser.Node) string {
	if node == nil || node.Next == nil || len(node.Next.Children) == 0 || node.Next.Children[0] == nil {
		return ""
	}
	return node.Next.Children[0].Value
}

func normalizeStageRef(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func isPreviouslyDefinedStageRef(from string, stageIndex int, defined map[string]struct{}) bool {
	if idx, err := strconv.Atoi(from); err == nil {
		return idx >= 0 && idx < stageIndex
	}

	_, ok := defined[normalizeStageRef(from)]
	return ok
}

func isExternalCopySource(from string) bool {
	return strings.Contains(from, ":")
}

func ScanRunCommandsWithPOSIXShell(
	input rules.LintInput,
	callback runcheck.RunCommandCallback,
) []rules.Violation {
	return runcheck.ScanRunCommandsWithPOSIXShell(input, callback)
}

type PackageManagerRuleConfig = runcheck.CommandFlagRuleConfig

func CheckPackageManagerFlag(
	input rules.LintInput,
	meta rules.RuleMetadata,
	config PackageManagerRuleConfig,
) []rules.Violation {
	return runcheck.CheckCommandFlag(input, meta, config)
}
