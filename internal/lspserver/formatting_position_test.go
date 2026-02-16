package lspserver

import (
	"testing"

	"github.com/stretchr/testify/assert"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
)

func TestPositionAtOffset_UsesUTF16CodeUnits(t *testing.T) {
	t.Parallel()

	content := []byte("a🙂b\nc𝄞d")

	tests := []struct {
		name   string
		offset int
		want   protocol.Position
	}{
		{name: "start", offset: 0, want: protocol.Position{Line: 0, Character: 0}},

		// Line 0: "a🙂b"
		// 'a' = 1 UTF-16 code unit, '🙂' (U+1F642) = 2, 'b' = 1.
		{name: "after a", offset: len([]byte("a")), want: protocol.Position{Line: 0, Character: 1}},
		{name: "after a+emoji", offset: len([]byte("a🙂")), want: protocol.Position{Line: 0, Character: 3}},
		{name: "after line0", offset: len([]byte("a🙂b")), want: protocol.Position{Line: 0, Character: 4}},

		// After newline.
		{name: "after newline", offset: len([]byte("a🙂b\n")), want: protocol.Position{Line: 1, Character: 0}},

		// Line 1: "c𝄞d"
		// 'c' = 1, '𝄞' (U+1D11E) = 2, 'd' = 1.
		{name: "after c", offset: len([]byte("a🙂b\nc")), want: protocol.Position{Line: 1, Character: 1}},
		{name: "after c+music", offset: len([]byte("a🙂b\nc𝄞")), want: protocol.Position{Line: 1, Character: 3}},
		{name: "end", offset: len([]byte("a🙂b\nc𝄞d")), want: protocol.Position{Line: 1, Character: 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, positionAtOffset(content, tt.offset))
		})
	}
}

func TestPositionAtOffset_BMPMultiByteRuneCountsAsOne(t *testing.T) {
	t.Parallel()

	content := []byte("éx") // U+00E9 is 2 bytes in UTF-8, but 1 UTF-16 code unit.
	offset := len([]byte("é"))
	assert.Equal(t, protocol.Position{Line: 0, Character: 1}, positionAtOffset(content, offset))
}

func TestPositionAtOffset_CombiningMarkCountsSeparately(t *testing.T) {
	t.Parallel()

	content := []byte("e\u0301") // "e" + combining acute accent.
	offset := len(content)
	assert.Equal(t, protocol.Position{Line: 0, Character: 2}, positionAtOffset(content, offset))
}
