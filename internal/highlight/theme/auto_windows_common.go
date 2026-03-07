package theme

const (
	windowsPersonalizeKey    = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	appsUseLightThemeName    = "AppsUseLightTheme"
	systemUsesLightThemeName = "SystemUsesLightTheme"
)

func resolveWindowsThemePreference(getValue func(string) (uint64, error)) (Mode, bool) {
	for _, name := range []string{appsUseLightThemeName, systemUsesLightThemeName} {
		value, err := getValue(name)
		if err != nil {
			continue
		}

		switch value {
		case 0:
			return ModeDark, true
		case 1:
			return ModeLight, true
		}
	}

	return ModeAuto, false
}
