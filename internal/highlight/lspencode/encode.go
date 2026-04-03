package lspencode

import (
	"github.com/wharflab/tally/internal/highlight/core"
)

var Legend = struct {
	TokenTypes     []string
	TokenModifiers []string
}{
	TokenTypes: []string{
		string(core.TokenKeyword),
		string(core.TokenComment),
		string(core.TokenString),
		string(core.TokenNumber),
		string(core.TokenOperator),
		string(core.TokenVariable),
		string(core.TokenParameter),
		string(core.TokenProperty),
		string(core.TokenFunction),
	},
	TokenModifiers: []string{
		"declaration",
		"readonly",
		"documentation",
		"defaultLibrary",
	},
}

var tokenTypeIndex = map[core.TokenType]uint32{
	core.TokenKeyword:   0,
	core.TokenComment:   1,
	core.TokenString:    2,
	core.TokenNumber:    3,
	core.TokenOperator:  4,
	core.TokenVariable:  5,
	core.TokenParameter: 6,
	core.TokenProperty:  7,
	core.TokenFunction:  8,
}

func Encode(tokens []core.Token) []uint32 {
	if len(tokens) == 0 {
		return nil
	}

	data := make([]uint32, 0, len(tokens)*5)
	prevLine := 0
	prevStart := 0
	for i, tok := range tokens {
		deltaLine := tok.Line
		deltaStart := tok.StartCol
		if i > 0 {
			deltaLine -= prevLine
			if deltaLine == 0 {
				deltaStart -= prevStart
			}
		}

		data = append(data,
			toUint32(deltaLine),
			toUint32(deltaStart),
			toUint32(tok.EndCol-tok.StartCol),
			tokenTypeIndex[tok.Type],
			modifierMask(tok.Modifiers),
		)
		prevLine = tok.Line
		prevStart = tok.StartCol
	}
	return data
}

func toUint32(v int) uint32 {
	const maxUint32 = int(^uint32(0))
	if v <= 0 {
		return 0
	}
	if v > maxUint32 {
		return ^uint32(0)
	}
	return uint32(v)
}

func modifierMask(mods uint32) uint32 {
	var mask uint32
	if mods&core.ModDeclaration != 0 {
		mask |= 1 << 0
	}
	if mods&core.ModReadonly != 0 {
		mask |= 1 << 1
	}
	if mods&core.ModDocumentation != 0 {
		mask |= 1 << 2
	}
	if mods&core.ModDefaultLibrary != 0 {
		mask |= 1 << 3
	}
	return mask
}
