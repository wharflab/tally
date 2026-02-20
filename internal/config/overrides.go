package config

import (
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// ConfigurationPreference controls how editor-provided overrides interact with
// filesystem config discovery.
//
// This is primarily used by editor integrations (LSP) to decide whether
// VS Code settings or `.tally.toml` / `tally.toml` should take precedence.
type ConfigurationPreference string

const (
	ConfigurationPreferenceEditorFirst     ConfigurationPreference = "editorFirst"
	ConfigurationPreferenceFilesystemFirst ConfigurationPreference = "filesystemFirst"
	ConfigurationPreferenceEditorOnly      ConfigurationPreference = "editorOnly"
)

func normalizeConfigurationPreference(p ConfigurationPreference) ConfigurationPreference {
	switch p {
	case ConfigurationPreferenceEditorFirst, ConfigurationPreferenceFilesystemFirst, ConfigurationPreferenceEditorOnly:
		return p
	default:
		return ConfigurationPreferenceEditorFirst
	}
}

// LoadWithOverrides loads configuration for a target file path with an optional
// overrides map applied according to preference.
//
// Overrides are expected to use the same (nested) shape as the TOML config file,
// for example:
//
//	overrides := map[string]any{
//	  "output": map[string]any{"format": "json"},
//	  "rules": map[string]any{"include": []any{"tally/*"}},
//	}
//
// Precedence:
//
// - editorFirst: defaults → filesystem config → env → overrides
// - filesystemFirst: defaults → overrides → filesystem config → env
// - editorOnly: defaults → env → overrides (filesystem discovery skipped)
func LoadWithOverrides(targetPath string, overrides map[string]any, preference ConfigurationPreference) (*Config, error) {
	preference = normalizeConfigurationPreference(preference)

	configPath := ""
	if preference != ConfigurationPreferenceEditorOnly {
		configPath = Discover(targetPath)
	}
	return loadWithConfigPathAndOverrides(configPath, overrides, preference)
}

func loadWithConfigPathAndOverrides(
	configPath string,
	overrides map[string]any,
	preference ConfigurationPreference,
) (*Config, error) {
	preference = normalizeConfigurationPreference(preference)

	k := koanf.New(".")

	// 1) Defaults
	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		return nil, err
	}

	// 2) Apply sources in configured precedence order.
	switch preference {
	case ConfigurationPreferenceEditorOnly:
		if err := loadEnv(k); err != nil {
			return nil, err
		}
		if err := loadOverrides(k, overrides); err != nil {
			return nil, err
		}
	case ConfigurationPreferenceFilesystemFirst:
		if err := loadOverrides(k, overrides); err != nil {
			return nil, err
		}
		if err := loadConfigFile(k, configPath); err != nil {
			return nil, err
		}
		if err := loadEnv(k); err != nil {
			return nil, err
		}
	case ConfigurationPreferenceEditorFirst:
		if err := loadConfigFile(k, configPath); err != nil {
			return nil, err
		}
		if err := loadEnv(k); err != nil {
			return nil, err
		}
		if err := loadOverrides(k, overrides); err != nil {
			return nil, err
		}
	}

	// 3) Validate merged raw config and decode.
	cfg, err := decodeConfig(k.Raw())
	if err != nil {
		return nil, err
	}

	cfg.ConfigFile = configPath
	return cfg, nil
}

func loadConfigFile(k *koanf.Koanf, configPath string) error {
	if configPath == "" {
		return nil
	}
	return k.Load(file.Provider(configPath), toml.Parser())
}

func loadEnv(k *koanf.Koanf) error {
	return k.Load(env.Provider(".", env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: envKeyTransform,
	}), nil)
}

func loadOverrides(k *koanf.Koanf, overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}
	return k.Load(confmap.Provider(overrides, ""), nil)
}
