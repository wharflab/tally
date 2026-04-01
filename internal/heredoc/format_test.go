package heredoc

import (
	"slices"
	"testing"
)

func TestPowerShellBodyLines_DeduplicatesEquivalentPreludeAssignments(t *testing.T) {
	t.Parallel()

	commands := []string{
		`$ErrorActionPreference="Stop"`,
		`$PSNativeCommandUseErrorActionPreference=$true`,
		`Write-Host ok`,
	}

	got := powerShellBodyLines(commands)
	want := []string{
		`$ErrorActionPreference="Stop"`,
		`$PSNativeCommandUseErrorActionPreference=$true`,
		`Write-Host ok`,
	}

	if !slices.Equal(got, want) {
		t.Fatalf("powerShellBodyLines() = %#v, want %#v", got, want)
	}
}

func TestPowerShellBodyLines_AddsPreludeWhenMissing(t *testing.T) {
	t.Parallel()

	got := powerShellBodyLines([]string{`Write-Host ok`})
	want := []string{
		"$ErrorActionPreference = 'Stop'",
		"$PSNativeCommandUseErrorActionPreference = $true",
		`Write-Host ok`,
	}

	if !slices.Equal(got, want) {
		t.Fatalf("powerShellBodyLines() = %#v, want %#v", got, want)
	}
}
