package theme

import "testing"

func TestResolveAutoMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		goos          string
		envTheme      string
		hasDark       bool
		want          Mode
		wantProbeCall bool
	}{
		{
			name:          "windows defaults to dark without probing",
			goos:          "windows",
			want:          ModeDark,
			wantProbeCall: false,
		},
		{
			name:          "windows honors explicit light env override",
			goos:          "windows",
			envTheme:      "light",
			want:          ModeLight,
			wantProbeCall: false,
		},
		{
			name:          "windows honors explicit dark env override",
			goos:          "windows",
			envTheme:      "dark",
			want:          ModeDark,
			wantProbeCall: false,
		},
		{
			name:          "non-windows uses dark probe result",
			goos:          "linux",
			hasDark:       true,
			want:          ModeDark,
			wantProbeCall: true,
		},
		{
			name:          "non-windows uses light probe result",
			goos:          "linux",
			hasDark:       false,
			want:          ModeLight,
			wantProbeCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			got := resolveAutoMode(tt.goos, tt.envTheme, func() bool {
				called = true
				return tt.hasDark
			})

			if got != tt.want {
				t.Fatalf("resolveAutoMode(%q, %q) = %q, want %q", tt.goos, tt.envTheme, got, tt.want)
			}
			if called != tt.wantProbeCall {
				t.Fatalf("resolveAutoMode(%q, %q) probe called = %v, want %v", tt.goos, tt.envTheme, called, tt.wantProbeCall)
			}
		})
	}
}
