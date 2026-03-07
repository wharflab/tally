//go:build windows

package theme

import "golang.org/x/sys/windows/registry"

func resolvePlatformAutoMode() Mode {
	mode, ok := resolveWindowsThemePreference(windowsThemePreferenceValue)
	if !ok {
		return ModeDark
	}
	return mode
}

func windowsThemePreferenceValue(name string) (uint64, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsPersonalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return 0, err
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue(name)
	if err != nil {
		return 0, err
	}
	return value, nil
}
