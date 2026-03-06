package renderansi

import (
	"strings"
	"testing"

	highlightcore "github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/theme"
)

func TestRenderLine_OverlayPreservesTokenColors(t *testing.T) {
	t.Parallel()

	rendered := RenderLine(
		"$(printenv ARIA2_PORT)",
		[]highlightcore.Token{
			{Line: 0, StartCol: 0, EndCol: 11, Type: highlightcore.TokenVariable},
			{Line: 0, StartCol: 11, EndCol: 21, Type: highlightcore.TokenProperty},
		},
		theme.Resolve(true, "dark"),
		&Overlay{StartCol: 0, EndCol: 11},
	)

	if !strings.Contains(rendered, "\x1b[") {
		t.Fatal("expected ANSI styling in rendered output")
	}
	if strings.Contains(rendered, "[7m") || strings.Contains(rendered, ";7m") || strings.Contains(rendered, ";7;") {
		t.Fatalf("expected overlay to avoid reverse-video styling, got %q", rendered)
	}
	if !strings.Contains(rendered, "[4m") && !strings.Contains(rendered, ";4m") && !strings.Contains(rendered, ";4;") {
		t.Fatalf("expected overlay to underline diagnostic span, got %q", rendered)
	}
}
