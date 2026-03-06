package renderansi

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/theme"
)

type Overlay struct {
	StartCol int
	EndCol   int
}

func RenderLine(line string, tokens []core.Token, palette theme.Palette, overlay *Overlay) string {
	if len(tokens) == 0 && overlay == nil {
		return line
	}

	runes := []rune(line)
	bounds := make([]int, 0, len(tokens)*2+2)
	for _, tok := range tokens {
		bounds = append(bounds, tok.StartCol, tok.EndCol)
	}
	if overlay != nil {
		bounds = append(bounds, overlay.StartCol, overlay.EndCol)
	}
	bounds = append(bounds, 0, len(runes))
	slices.Sort(bounds)

	uniq := bounds[:0]
	for _, bound := range bounds {
		if bound < 0 {
			continue
		}
		if bound > len(runes) {
			bound = len(runes)
		}
		if len(uniq) == 0 || uniq[len(uniq)-1] != bound {
			uniq = append(uniq, bound)
		}
	}

	var out strings.Builder
	for i := range len(uniq) - 1 {
		start, end := uniq[i], uniq[i+1]
		if end <= start {
			continue
		}
		segment := string(runes[start:end])
		style := palette.Base
		if tok, ok := coveringToken(tokens, start, end); ok {
			if tokenStyle, ok := palette.ByToken[tok.Type]; ok {
				style = tokenStyle
			}
		}
		if overlay != nil && overlay.StartCol <= start && overlay.EndCol >= end {
			style = style.Inherit(palette.Overlay)
		}
		out.WriteString(style.Render(segment))
	}
	return out.String()
}

func coveringToken(tokens []core.Token, start, end int) (core.Token, bool) {
	for _, tok := range tokens {
		if tok.StartCol <= start && tok.EndCol >= end {
			return tok, true
		}
	}
	return core.Token{}, false
}

func Inherit(a, b lipgloss.Style) lipgloss.Style {
	return a.Inherit(b)
}
