//go:build containers_image_openpgp && containers_image_storage_stub && containers_image_docker_daemon_stub

package registry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/blobinfocache/memory"
	"go.podman.io/image/v5/pkg/cli/environment"
	"go.podman.io/image/v5/types"

	godigest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

func init() {
	NewDefaultResolver = func() ImageResolver {
		return NewContainersResolver()
	}
}

// ContainersResolver uses go.podman.io/image/v5 (containers/image) to resolve
// image configs from OCI/Docker registries. It respects registries.conf and
// auth.json via types.SystemContext.
type ContainersResolver struct {
	sysCtx    *types.SystemContext
	blobCache types.BlobInfoCache
}

// NewContainersResolver creates a resolver using the default system context.
// It respects CONTAINERS_REGISTRIES_CONF / REGISTRIES_CONFIG_PATH environment
// variables for registry mirrors and redirects.
func NewContainersResolver() *ContainersResolver {
	sysCtx := &types.SystemContext{}
	// Apply environment variable overrides for registries.conf discovery.
	// Error is ignored: env var overrides are optional and missing vars are not fatal.
	_ = environment.UpdateRegistriesConf(sysCtx)
	return &ContainersResolver{sysCtx: sysCtx, blobCache: memory.New()}
}

// NewContainersResolverWithContext creates a resolver with a custom system context.
func NewContainersResolverWithContext(sysCtx *types.SystemContext) *ContainersResolver {
	if sysCtx == nil {
		sysCtx = &types.SystemContext{}
	}
	return &ContainersResolver{sysCtx: sysCtx, blobCache: memory.New()}
}

// ResolveConfig resolves image config from the registry.
func (r *ContainersResolver) ResolveConfig(ctx context.Context, ref string, platform string) (ImageConfig, error) {
	// Parse the image reference.
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return ImageConfig{}, &NotFoundError{Ref: ref, Err: fmt.Errorf("invalid reference: %w", err)}
	}
	// Ensure we have a tag or digest.
	named = reference.TagNameOnly(named)

	// Create a docker reference.
	dockerRef, err := docker.NewReference(named)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	// Set up system context with platform selection.
	sysCtx := *r.sysCtx
	if platform != "" {
		parts := strings.SplitN(platform, "/", 3)
		if len(parts) >= 1 {
			sysCtx.OSChoice = parts[0]
		}
		if len(parts) >= 2 {
			sysCtx.ArchitectureChoice = parts[1]
		}
		if len(parts) >= 3 {
			sysCtx.VariantChoice = parts[2]
		}
	}

	// Create image source.
	src, err := dockerRef.NewImageSource(ctx, &sysCtx)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}
	defer src.Close()

	// Get the manifest.
	rawManifest, mimeType, err := src.GetManifest(ctx, nil)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	// If it's a manifest list/index, select the matching platform entry.
	if manifest.MIMETypeIsMultiImage(mimeType) {
		return r.resolveFromIndex(ctx, src, rawManifest, mimeType, ref, platform, &sysCtx)
	}

	// Single manifest: get config directly.
	return r.resolveFromManifest(ctx, src, rawManifest, mimeType, ref, platform)
}

func (r *ContainersResolver) resolveFromIndex(
	ctx context.Context,
	src types.ImageSource,
	rawIndex []byte,
	indexMIME string,
	ref, wantPlatform string,
	sysCtx *types.SystemContext,
) (ImageConfig, error) {
	list, err := manifest.ListFromBlob(rawIndex, indexMIME)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	// Use ChooseInstance for platform selection (respects SystemContext).
	chosen, err := list.ChooseInstance(sysCtx)
	if err != nil {
		// Platform mismatch: try to collect available platforms.
		available := collectAvailablePlatforms(list)
		return ImageConfig{}, &PlatformMismatchError{
			Ref:       ref,
			Requested: wantPlatform,
			Available: available,
			Err:       err,
		}
	}

	// Get the platform-specific manifest.
	rawManifest, mimeType, err := src.GetManifest(ctx, &chosen)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	cfg, err := r.resolveFromManifest(ctx, src, rawManifest, mimeType, ref, "")
	if err != nil {
		return cfg, err
	}
	cfg.Digest = chosen.String()
	return cfg, nil
}

func (r *ContainersResolver) resolveFromManifest(
	ctx context.Context,
	src types.ImageSource,
	rawManifest []byte,
	mimeType string,
	ref, wantPlatform string,
) (ImageConfig, error) {
	man, err := manifest.FromBlob(rawManifest, mimeType)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	configDigest := man.ConfigInfo().Digest
	configBlob, _, err := src.GetBlob(ctx, types.BlobInfo{Digest: configDigest}, r.blobCache)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}
	defer configBlob.Close()

	// Read and parse the OCI config.
	configBytes, err := readAll(configBlob, 1<<20) // 1MB limit
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	ociConfig, err := parseOCIConfig(configBytes)
	if err != nil {
		return ImageConfig{}, classifyContainersError(ref, err)
	}

	// Use the manifest digest (not the config blob digest) for consistency
	// with the multi-arch path where chosen.String() is the manifest digest.
	manifestDigest := godigest.FromBytes(rawManifest)

	imgCfg := ImageConfig{
		Env:            parseEnvList(ociConfig.Config.Env),
		OS:             ociConfig.OS,
		Arch:           ociConfig.Architecture,
		Variant:        ociConfig.Variant,
		Digest:         manifestDigest.String(),
		HasHealthcheck: extractHasHealthcheck(configBytes),
	}

	// Platform mismatch check for single-manifest images.
	if wantPlatform != "" {
		wantOS, wantArch, wantVariant := parsePlatformString(wantPlatform)
		if !matchesPlatformValues(ociConfig.OS, ociConfig.Architecture, ociConfig.Variant, wantOS, wantArch, wantVariant) {
			return imgCfg, &PlatformMismatchError{
				Ref:       ref,
				Requested: wantPlatform,
				Available: []string{formatPlatformParts(ociConfig.OS, ociConfig.Architecture, ociConfig.Variant)},
			}
		}
	}

	return imgCfg, nil
}

// collectAvailablePlatforms extracts available platforms from a manifest list.
func collectAvailablePlatforms(list manifest.List) []string {
	var platforms []string
	for _, d := range list.Instances() {
		inst, err := list.Instance(d)
		if err != nil || inst.ReadOnly.Platform == nil {
			continue
		}
		platforms = append(platforms, formatPlatform(inst.ReadOnly.Platform))
	}
	return platforms
}

func matchesPlatformValues(imgOS, imgArch, imgVariant, wantOS, wantArch, wantVariant string) bool {
	if wantOS != "" && !strings.EqualFold(imgOS, wantOS) {
		return false
	}
	if wantArch != "" && !strings.EqualFold(imgArch, wantArch) {
		return false
	}
	if wantVariant != "" && !strings.EqualFold(imgVariant, wantVariant) {
		return false
	}
	return true
}

func formatPlatform(p *imgspecv1.Platform) string {
	if p == nil {
		return ""
	}
	return formatPlatformParts(p.OS, p.Architecture, p.Variant)
}

func formatPlatformParts(os, arch, variant string) string {
	s := os + "/" + arch
	if variant != "" {
		s += "/" + variant
	}
	return s
}

func parsePlatformString(platform string) (os, arch, variant string) {
	parts := strings.SplitN(platform, "/", 3)
	if len(parts) >= 1 {
		os = parts[0]
	}
	if len(parts) >= 2 {
		arch = parts[1]
	}
	if len(parts) >= 3 {
		variant = parts[2]
	}
	return
}

// parseEnvList converts OCI config env ([]string of "KEY=VALUE") to a map.
func parseEnvList(envList []string) map[string]string {
	m := make(map[string]string, len(envList))
	for _, e := range envList {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			m[k] = v
		}
	}
	return m
}

// classifyContainersError wraps errors from containers/image into typed errors.
// It uses typed error matching (errcode.ErrorCoder, docker.ErrUnauthorizedForCredentials,
// docker.UnexpectedHTTPStatusError) where possible, falling back to string matching
// only for errors that don't carry structured type information.
func classifyContainersError(ref string, err error) error {
	if err == nil {
		return nil
	}

	// Context errors during registry access typically mean the registry was
	// unreachable or rate-limiting within the per-request budget (e.g., Docker Hub
	// returns HTTP 429 and containers/image retries with exponential backoff until
	// the deadline). Wrap as NetworkError so the retry/skip logic classifies it
	// correctly, rather than surfacing raw "context deadline exceeded".
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &NetworkError{Err: fmt.Errorf("registry %s: %w", ref, err)}
	}

	// Typed error: containers/image returns ErrUnauthorizedForCredentials for 401.
	var authCredErr docker.ErrUnauthorizedForCredentials
	if errors.As(err, &authCredErr) {
		return &AuthError{Err: err}
	}

	// Typed error: containers/image returns ErrTooManyRequests for 429.
	if errors.Is(err, docker.ErrTooManyRequests) {
		return &NetworkError{Err: fmt.Errorf("registry %s: %w", ref, err)}
	}

	// Typed error: errcode.ErrorCoder carries the registry API error code.
	var ecoder errcode.ErrorCoder
	if errors.As(err, &ecoder) {
		switch ecoder.ErrorCode() {
		case errcode.ErrorCodeUnauthorized, errcode.ErrorCodeDenied:
			return &AuthError{Err: err}
		case v2.ErrorCodeManifestUnknown, v2.ErrorCodeNameUnknown, v2.ErrorCodeBlobUnknown:
			return &NotFoundError{Ref: ref, Err: err}
		case errcode.ErrorCodeTooManyRequests, errcode.ErrorCodeUnavailable:
			return &NetworkError{Err: err}
		}
	}

	// Typed error: UnexpectedHTTPStatusError carries the HTTP status code directly.
	var httpErr docker.UnexpectedHTTPStatusError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.StatusCode == 401 || httpErr.StatusCode == 403:
			return &AuthError{Err: err}
		case httpErr.StatusCode == 404:
			return &NotFoundError{Ref: ref, Err: err}
		default:
			return &NetworkError{Err: err}
		}
	}

	// Typed error: net.Error for network-level failures.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &NetworkError{Err: err}
	}

	// Fallback: string matching for errors that don't carry typed information.
	errStr := err.Error()
	if strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "authentication required") ||
		strings.Contains(errStr, "denied") {
		return &AuthError{Err: err}
	}
	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "manifest unknown") ||
		strings.Contains(errStr, "name unknown") {
		return &NotFoundError{Ref: ref, Err: err}
	}

	return &NetworkError{Err: err}
}
