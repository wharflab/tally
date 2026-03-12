package patch

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Meta summarizes the accepted patch for downstream heuristic checks.
type Meta struct {
	OldName          string
	NewName          string
	AddedLines       []string
	DeletedLines     []string
	AddedLineCount   int
	DeletedLineCount int
}

// ParseAndApply parses a single-file unified diff and applies it strictly to base.
func ParseAndApply(base []byte, patchText string) ([]byte, Meta, error) {
	files, preamble, err := gitdiff.Parse(strings.NewReader(normalizeLF(patchText)))
	if err != nil {
		return nil, Meta{}, fmt.Errorf("parse patch: %w", err)
	}
	if strings.TrimSpace(preamble) != "" {
		return nil, Meta{}, errors.New("unexpected content before patch header")
	}
	if len(files) != 1 {
		return nil, Meta{}, fmt.Errorf("patch must modify exactly one file, got %d", len(files))
	}

	file := files[0]
	if file == nil {
		return nil, Meta{}, errors.New("patch did not contain a file diff")
	}
	if file.IsNew || file.IsDelete {
		return nil, Meta{}, errors.New("patch must not create or delete files")
	}
	if file.IsCopy || file.IsRename {
		return nil, Meta{}, errors.New("patch must not copy or rename files")
	}
	if file.IsBinary {
		return nil, Meta{}, errors.New("patch must be text-only")
	}
	if len(file.TextFragments) == 0 {
		return nil, Meta{}, errors.New("patch must include at least one text hunk")
	}

	meta := collectMeta(file)

	var out bytes.Buffer
	if err := gitdiff.Apply(&out, bytes.NewReader(base), file); err != nil {
		return nil, meta, fmt.Errorf("apply patch: %w", err)
	}

	proposed := alignTrailingNewline(base, out.Bytes())
	return proposed, meta, nil
}

func collectMeta(file *gitdiff.File) Meta {
	meta := Meta{
		OldName: file.OldName,
		NewName: file.NewName,
	}
	for _, frag := range file.TextFragments {
		for _, line := range frag.Lines {
			text := strings.TrimSuffix(line.Line, "\n")
			switch line.Op {
			case gitdiff.OpAdd:
				meta.AddedLines = append(meta.AddedLines, text)
				meta.AddedLineCount++
			case gitdiff.OpDelete:
				meta.DeletedLines = append(meta.DeletedLines, text)
				meta.DeletedLineCount++
			case gitdiff.OpContext:
				// Context lines do not contribute to patch heuristics.
			}
		}
	}
	return meta
}

func normalizeLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func alignTrailingNewline(base, proposed []byte) []byte {
	baseHasNewline := len(base) > 0 && base[len(base)-1] == '\n'
	proposedHasNewline := len(proposed) > 0 && proposed[len(proposed)-1] == '\n'

	switch {
	case baseHasNewline && !proposedHasNewline:
		return append(proposed, '\n')
	case !baseHasNewline && proposedHasNewline:
		return proposed[:len(proposed)-1]
	default:
		return proposed
	}
}
