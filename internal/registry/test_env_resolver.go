//go:build containers_image_openpgp && containers_image_storage_stub && containers_image_docker_daemon_stub

package registry

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"strings"
	"time"
)

const testImageConfigsEnv = "TALLY_TEST_IMAGE_CONFIGS"

type testImageResolver struct {
	loadErr error
	images  map[string]testImageEntry
}

type testImageConfigFile struct {
	Images map[string]testImageEntry `json:"images"`
}

type testImageEntry struct {
	Configs []testImageConfig `json:"configs"`
	Delay   string            `json:"delay,omitempty"`

	delay time.Duration
}

type testImageConfig struct {
	Env            map[string]string `json:"env,omitempty"`
	OS             string            `json:"os"`
	Arch           string            `json:"arch"`
	Variant        string            `json:"variant,omitempty"`
	Digest         string            `json:"digest,omitempty"`
	HasHealthcheck bool              `json:"hasHealthcheck,omitempty"`
	WorkingDir     string            `json:"workingDir,omitempty"`
	Shell          []string          `json:"shell,omitempty"`
}

func newTestImageResolverFromEnv() ImageResolver {
	path := os.Getenv(testImageConfigsEnv)
	if path == "" {
		return nil
	}

	resolver := &testImageResolver{}
	// #nosec G703 -- this integration-test hook intentionally reads the TestMain-provided config path.
	data, err := os.ReadFile(path)
	if err != nil {
		resolver.loadErr = fmt.Errorf("read %s: %w", testImageConfigsEnv, err)
		return resolver
	}

	var cfg testImageConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		resolver.loadErr = fmt.Errorf("parse %s: %w", testImageConfigsEnv, err)
		return resolver
	}

	resolver.images = make(map[string]testImageEntry, len(cfg.Images))
	for ref, entry := range cfg.Images {
		if entry.Delay != "" {
			delay, err := time.ParseDuration(entry.Delay)
			if err != nil {
				resolver.loadErr = fmt.Errorf("parse delay for %s: %w", ref, err)
				return resolver
			}
			entry.delay = delay
		}
		resolver.images[normalizeTestImageRef(ref)] = entry
	}
	return resolver
}

func (r *testImageResolver) ResolveConfig(ctx context.Context, ref, platform string) (ImageConfig, error) {
	if r.loadErr != nil {
		return ImageConfig{}, r.loadErr
	}

	key := normalizeTestImageRef(ref)
	entry, ok := r.images[key]
	if !ok {
		return ImageConfig{}, &NotFoundError{Ref: ref, Err: fmt.Errorf("missing test image config for %s", key)}
	}
	if entry.delay > 0 {
		select {
		case <-time.After(entry.delay):
		case <-ctx.Done():
			return ImageConfig{}, ctx.Err()
		}
	}

	if platform == "" && len(entry.Configs) > 0 {
		return imageConfigFromTestConfig(entry.Configs[0]), nil
	}
	for _, cfg := range entry.Configs {
		if testPlatformMatches(cfg, platform) {
			return imageConfigFromTestConfig(cfg), nil
		}
	}
	return ImageConfig{}, &PlatformMismatchError{
		Ref:       ref,
		Requested: platform,
		Available: availableTestPlatforms(entry.Configs),
	}
}

func imageConfigFromTestConfig(cfg testImageConfig) ImageConfig {
	return ImageConfig(cfg)
}

func testPlatformMatches(cfg testImageConfig, platform string) bool {
	osName, arch, variant := splitTestPlatform(platform)
	if osName != "" && !strings.EqualFold(cfg.OS, osName) {
		return false
	}
	if arch != "" && !strings.EqualFold(cfg.Arch, arch) {
		return false
	}
	if variant != "" && !strings.EqualFold(cfg.Variant, variant) {
		return false
	}
	return true
}

func availableTestPlatforms(configs []testImageConfig) []string {
	out := make([]string, 0, len(configs))
	for _, cfg := range configs {
		p := cfg.OS + "/" + cfg.Arch
		if cfg.Variant != "" {
			p += "/" + cfg.Variant
		}
		out = append(out, p)
	}
	return out
}

func splitTestPlatform(platform string) (string, string, string) {
	var osName, arch, variant string
	parts := strings.SplitN(platform, "/", 3)
	if len(parts) >= 1 {
		osName = parts[0]
	}
	if len(parts) >= 2 {
		arch = parts[1]
	}
	if len(parts) >= 3 {
		variant = parts[2]
	}
	return osName, arch, variant
}

func normalizeTestImageRef(ref string) string {
	ref = strings.TrimSpace(ref)
	name, tag := splitTestImageRefTag(ref)
	if tag == "" {
		tag = "latest"
	}

	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 1 || !isExplicitRegistryHost(parts[0]) {
		path := name
		if !strings.Contains(path, "/") {
			path = "library/" + path
		}
		return "docker.io/" + path + ":" + tag
	}
	return parts[0] + "/" + parts[1] + ":" + tag
}

func splitTestImageRefTag(ref string) (string, string) {
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	lastSlash := strings.LastIndex(ref, "/")
	lastColon := strings.LastIndex(ref, ":")
	if lastColon > lastSlash {
		return ref[:lastColon], ref[lastColon+1:]
	}
	return ref, ""
}

func isExplicitRegistryHost(host string) bool {
	return host == "localhost" || strings.Contains(host, ".") || strings.Contains(host, ":")
}
