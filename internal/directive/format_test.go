package directive

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatNextLine(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "# tally ignore=DL3008", FormatNextLine([]string{"DL3008"}, ""))
	assert.Equal(t, "# tally ignore=DL3008,DL3027", FormatNextLine([]string{"DL3008", "DL3027"}, ""))
	assert.Equal(t, "# tally ignore=DL3008;reason=false positive",
		FormatNextLine([]string{"DL3008"}, "false positive"))
}

func TestFormatGlobal(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "# tally global ignore=tally/max-lines", FormatGlobal([]string{"tally/max-lines"}, ""))
	assert.Equal(t, "# tally global ignore=DL3008;reason=TODO",
		FormatGlobal([]string{"DL3008"}, "TODO"))
}

func TestFormatNextLine_RoundTrip(t *testing.T) {
	t.Parallel()

	// A formatted directive should be parseable by the tallyPattern regex.
	text := FormatNextLine([]string{"DL3008", "tally/max-lines"}, "testing")
	matches := tallyPattern.FindStringSubmatch(text)
	assert.NotNil(t, matches, "formatted directive should match tallyPattern")
	assert.Empty(t, matches[1], "should not have 'global' capture")
	assert.Equal(t, "DL3008,tally/max-lines", matches[2])
	assert.Equal(t, "testing", matches[3])
}

func TestFormatGlobal_RoundTrip(t *testing.T) {
	t.Parallel()

	text := FormatGlobal([]string{"DL3008"}, "")
	matches := tallyPattern.FindStringSubmatch(text)
	assert.NotNil(t, matches, "formatted global directive should match tallyPattern")
	assert.Contains(t, matches[1], "global")
	assert.Equal(t, "DL3008", matches[2])
}

func TestAppendRule(t *testing.T) {
	t.Parallel()

	t.Run("simple append", func(t *testing.T) {
		t.Parallel()
		edit := AppendRule("# tally ignore=DL3008", "DL3027")
		assert.Equal(t, ",DL3027", edit.NewText)
		assert.Equal(t, 21, edit.Start)
		assert.Equal(t, 21, edit.End)
	})

	t.Run("before reason", func(t *testing.T) {
		t.Parallel()
		edit := AppendRule("# tally ignore=DL3008;reason=test", "DL3027")
		assert.Equal(t, ",DL3027", edit.NewText)
		assert.Equal(t, 21, edit.Start)
		assert.Equal(t, 21, edit.End)
	})

	t.Run("trims trailing spaces before reason", func(t *testing.T) {
		t.Parallel()
		edit := AppendRule("# tally ignore=DL3008   ;reason=test", "DL3027")
		assert.Equal(t, ",DL3027", edit.NewText)
		assert.Equal(t, 21, edit.Start)
		assert.Equal(t, 24, edit.End, "should replace the trailing spaces")
	})
}
