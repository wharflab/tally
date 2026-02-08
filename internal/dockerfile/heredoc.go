package dockerfile

import (
	"slices"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// HeredocKind classifies the type of heredoc based on its containing instruction.
type HeredocKind int

const (
	// HeredocKindUnknown indicates a heredoc in an unrecognized instruction.
	// This makes unclassified heredocs explicit rather than silently defaulting.
	HeredocKindUnknown HeredocKind = iota

	// HeredocKindScript indicates a heredoc in a RUN instruction.
	// The content is a shell script to be executed.
	HeredocKindScript

	// HeredocKindInlineSource indicates a heredoc in COPY or ADD instruction.
	// The content is an inline file that does not come from build context.
	// These are not affected by .dockerignore and don't require file existence.
	HeredocKindInlineSource
)

// String returns the string representation of the HeredocKind.
func (k HeredocKind) String() string {
	switch k {
	case HeredocKindUnknown:
		return "unknown"
	case HeredocKindScript:
		return "script"
	case HeredocKindInlineSource:
		return "inline-source"
	default:
		return "unknown"
	}
}

// HeredocInfo represents a heredoc extracted from a Dockerfile instruction.
// This provides structured access to heredoc content with type classification.
//
// BuildKit's parser.Heredoc is preserved in full, with additional context
// about which instruction contains the heredoc.
type HeredocInfo struct {
	// Heredoc is the BuildKit heredoc structure.
	// Contains Name, Content, Expand, Chomp, and FileDescriptor.
	parser.Heredoc

	// Kind classifies the heredoc based on its containing instruction.
	Kind HeredocKind

	// Instruction is the Dockerfile instruction containing this heredoc.
	// One of: RUN, COPY, ADD
	Instruction string

	// Line is the 0-based line number where the instruction starts.
	// The heredoc content follows after the instruction line.
	Line int
}

// ExtractHeredocs extracts all heredocs from a parsed Dockerfile.
// Heredocs are classified by their containing instruction:
//   - RUN heredocs are scripts (HeredocKindScript)
//   - COPY/ADD heredocs are inline sources (HeredocKindInlineSource)
//
// This classification is important for context-aware rules that need to
// distinguish between files from build context vs inline heredoc content.
func ExtractHeredocs(result *ParseResult) []HeredocInfo {
	if result == nil || result.AST == nil || result.AST.AST == nil {
		return nil
	}

	var heredocs []HeredocInfo

	// Walk the AST children - each child is a Dockerfile instruction
	// Heredocs are attached to the instruction node, not to nested nodes
	for _, node := range result.AST.AST.Children {
		if len(node.Heredocs) == 0 {
			continue
		}

		instruction := node.Value
		kind := classifyHeredoc(instruction)
		line := node.StartLine - 1 // Convert to 0-based

		for _, hd := range node.Heredocs {
			heredocs = append(heredocs, HeredocInfo{
				Heredoc:     hd,
				Kind:        kind,
				Instruction: instruction,
				Line:        line,
			})
		}
	}

	return heredocs
}

// classifyHeredoc determines the kind of heredoc based on instruction.
// Returns HeredocKindUnknown for unrecognized instructions, allowing callers
// to detect and handle unclassified heredocs explicitly.
func classifyHeredoc(instruction string) HeredocKind {
	switch instruction {
	case "RUN":
		return HeredocKindScript
	case "COPY", "ADD":
		return HeredocKindInlineSource
	default:
		// Return unknown for unrecognized instructions rather than guessing.
		// Currently, only RUN, COPY, and ADD support heredocs in Docker.
		return HeredocKindUnknown
	}
}

// HasHeredocs returns true if the parse result contains any heredocs.
func HasHeredocs(result *ParseResult) bool {
	if result == nil || result.AST == nil || result.AST.AST == nil {
		return false
	}

	return slices.ContainsFunc(result.AST.AST.Children, func(node *parser.Node) bool {
		return len(node.Heredocs) > 0
	})
}

// IsInlineSource returns true if the heredoc is inline content (COPY/ADD).
// Inline sources are not affected by .dockerignore and don't require
// file existence in the build context.
func (h HeredocInfo) IsInlineSource() bool {
	return h.Kind == HeredocKindInlineSource
}

// IsScript returns true if the heredoc is a shell script (RUN).
func (h HeredocInfo) IsScript() bool {
	return h.Kind == HeredocKindScript
}
