package theme

import (
	"os"
	"testing"
)

func TestResolveAutoMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		envTheme     string
		platformMode Mode
		want         Mode
		wantCall     bool
	}{
		{
			name:         "uses platform mode for auto",
			platformMode: ModeDark,
			want:         ModeDark,
			wantCall:     true,
		},
		{
			name:         "honors explicit light env override",
			envTheme:     "light",
			platformMode: ModeDark,
			want:         ModeLight,
			wantCall:     false,
		},
		{
			name:         "honors explicit dark env override",
			envTheme:     "dark",
			platformMode: ModeLight,
			want:         ModeDark,
			wantCall:     false,
		},
		{
			name:         "falls back to platform mode for invalid env override",
			envTheme:     "not-a-real-theme",
			platformMode: ModeLight,
			want:         ModeLight,
			wantCall:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			got := resolveAutoMode(tt.envTheme, func() Mode {
				called = true
				return tt.platformMode
			})

			if got != tt.want {
				t.Fatalf("resolveAutoMode(%q) = %q, want %q", tt.envTheme, got, tt.want)
			}
			if called != tt.wantCall {
				t.Fatalf("resolveAutoMode(%q) platform called = %v, want %v", tt.envTheme, called, tt.wantCall)
			}
		})
	}
}

func TestResolveWindowsThemePreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values map[string]uint64
		want   Mode
		ok     bool
	}{
		{
			name:   "uses apps preference when available",
			values: map[string]uint64{appsUseLightThemeName: 1, systemUsesLightThemeName: 0},
			want:   ModeLight,
			ok:     true,
		},
		{
			name:   "falls back to system preference",
			values: map[string]uint64{systemUsesLightThemeName: 0},
			want:   ModeDark,
			ok:     true,
		},
		{
			name:   "ignores unsupported values",
			values: map[string]uint64{appsUseLightThemeName: 2, systemUsesLightThemeName: 3},
			want:   ModeAuto,
			ok:     false,
		},
		{
			name:   "returns false when nothing can be read",
			values: map[string]uint64{},
			want:   ModeAuto,
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := resolveWindowsThemePreference(func(name string) (uint64, error) {
				value, found := tt.values[name]
				if !found {
					return 0, os.ErrNotExist
				}
				return value, nil
			})

			if got != tt.want || ok != tt.ok {
				t.Fatalf("resolveWindowsThemePreference() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}
