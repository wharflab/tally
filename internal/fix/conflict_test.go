package fix

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestEditsOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a    rules.TextEdit
		b    rules.TextEdit
		want bool
	}{
		{
			name: "different files",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("a.txt", 1, 0, 1, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("b.txt", 1, 0, 1, 10)},
			want: false,
		},
		{
			name: "A before B same line",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 5)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 5, 1, 10)},
			want: false,
		},
		{
			name: "B before A same line",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 5, 1, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 5)},
			want: false,
		},
		{
			name: "A before B different lines",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 2, 0, 2, 10)},
			want: false,
		},
		{
			name: "overlapping same line",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 5, 1, 15)},
			want: true,
		},
		{
			name: "overlapping multi-line",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 3, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 2, 0, 4, 10)},
			want: true,
		},
		{
			name: "contained",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 20)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 5, 1, 10)},
			want: true,
		},
		{
			name: "zero-width insert at start of range - not overlapping",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 0)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 10)},
			want: false, // Zero-width [0,0) is before [0,10); position drift handled by column adjustment
		},
		{
			name: "zero-width insert at end of range - not overlapping",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 10, 1, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 10)},
			want: false, // Zero-width [10,10) is after [0,10)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := editsOverlap(tt.a, tt.b); got != tt.want {
				t.Errorf("editsOverlap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareEdits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a    rules.TextEdit
		b    rules.TextEdit
		want bool // true if a comes before b
	}{
		{
			name: "different lines",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 2, 0, 2, 10)},
			want: true,
		},
		{
			name: "same line different columns",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 5)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 5, 1, 10)},
			want: true,
		},
		{
			name: "reverse order",
			a:    rules.TextEdit{Location: rules.NewRangeLocation("f", 2, 0, 2, 10)},
			b:    rules.TextEdit{Location: rules.NewRangeLocation("f", 1, 0, 1, 10)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := compareEdits(tt.a, tt.b); got != tt.want {
				t.Errorf("compareEdits() = %v, want %v", got, tt.want)
			}
		})
	}
}
