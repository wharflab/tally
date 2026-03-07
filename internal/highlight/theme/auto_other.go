//go:build !windows

package theme

import (
	"os"

	"charm.land/lipgloss/v2"
)

func resolvePlatformAutoMode() Mode {
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return ModeDark
	}
	return ModeLight
}
