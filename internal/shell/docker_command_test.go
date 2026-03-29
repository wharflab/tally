package shell

import (
	"slices"
	"testing"
)

func TestDockerCommandNames(t *testing.T) {
	t.Parallel()

	shellScript := "env PORT=3000 exec next start"
	shellVariant := VariantBash

	tests := []struct {
		name         string
		cmdLine      []string
		prependShell bool
		variant      Variant
		want         []string
	}{
		{
			name:    "empty cmdline",
			variant: VariantBash,
			want:    nil,
		},
		{
			name:         "shell form delegates to command names with variant",
			cmdLine:      []string{shellScript},
			prependShell: true,
			variant:      shellVariant,
			want:         CommandNamesWithVariant(shellScript, shellVariant),
		},
		{
			name:    "exec form with absolute path",
			cmdLine: []string{"/usr/bin/foo", "serve"},
			variant: VariantBash,
			want:    []string{"foo"},
		},
		{
			name:    "exec form with relative path",
			cmdLine: []string{"bin/foo", "serve"},
			variant: VariantBash,
			want:    []string{"foo"},
		},
		{
			name:    "exec form with empty argv0",
			cmdLine: []string{""},
			variant: VariantBash,
			want:    nil,
		},
		{
			name:    "exec form with dot argv0",
			cmdLine: []string{"."},
			variant: VariantBash,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DockerCommandNames(tt.cmdLine, tt.prependShell, tt.variant)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("DockerCommandNames(%v, %t, %v) = %v, want %v", tt.cmdLine, tt.prependShell, tt.variant, got, tt.want)
			}
		})
	}
}
