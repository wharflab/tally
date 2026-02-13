// Package registry provides OCI registry integration for resolving base image
// configuration (env, platform, digest) via containers/image.
package registry

import (
	"context"
	"fmt"

	"github.com/tinovyatkin/tally/internal/async"
)

const registryResolverID = "registry"

// RegistryResolverID is the resolver ID for registry-based image resolution.
func RegistryResolverID() string { return registryResolverID }

// ImageResolver resolves image configuration from a registry.
type ImageResolver interface {
	// ResolveConfig resolves image config (env + resolved digest/platform)
	// for the given ref and platform (e.g., "linux/amd64").
	//
	// Error contract:
	//   - AuthError: 401/403, missing/expired creds
	//   - NetworkError: transient network failure
	//   - NotFoundError: ref/tag/manifest not found
	//   - PlatformMismatchError: image exists but no manifest matches platform
	ResolveConfig(ctx context.Context, ref string, platform string) (ImageConfig, error)
}

// ImageConfig holds resolved image metadata.
type ImageConfig struct {
	// Env is the image's environment variables (KEY=VALUE parsed to map).
	Env map[string]string

	// OS is the image's target OS (e.g., "linux").
	OS string

	// Arch is the image's target architecture (e.g., "amd64").
	Arch string

	// Variant is the image's architecture variant (e.g., "v8").
	Variant string

	// Digest is the resolved manifest digest.
	Digest string

	// HasHealthcheck is true if the image defines a HEALTHCHECK (CMD or CMD-SHELL).
	// False if HEALTHCHECK is NONE or absent.
	HasHealthcheck bool
}

// ResolveRequest is the typed input for the registry async resolver.
type ResolveRequest struct {
	Ref      string
	Platform string
}

// AuthError indicates authentication/authorization failure.
type AuthError struct{ Err error }

func (e *AuthError) Error() string                { return fmt.Sprintf("auth error: %v", e.Err) }
func (e *AuthError) Unwrap() error                { return e.Err }
func (e *AuthError) SkipReason() async.SkipReason { return async.SkipAuth }

// NetworkError indicates a transient network failure.
type NetworkError struct{ Err error }

func (e *NetworkError) Error() string                { return fmt.Sprintf("network error: %v", e.Err) }
func (e *NetworkError) Unwrap() error                { return e.Err }
func (e *NetworkError) SkipReason() async.SkipReason { return async.SkipNetwork }

// NotFoundError indicates the ref/tag/manifest was not found.
type NotFoundError struct {
	Ref string
	Err error
}

func (e *NotFoundError) Error() string                { return fmt.Sprintf("not found: %s: %v", e.Ref, e.Err) }
func (e *NotFoundError) Unwrap() error                { return e.Err }
func (e *NotFoundError) SkipReason() async.SkipReason { return async.SkipNotFound }

// PlatformMismatchError indicates the image exists but no manifest matches
// the requested platform. This is NOT a skip — it becomes a violation.
type PlatformMismatchError struct {
	Ref       string
	Requested string
	Available []string
	Err       error
}

func (e *PlatformMismatchError) Error() string {
	return fmt.Sprintf("platform mismatch for %s: requested %s, available %v", e.Ref, e.Requested, e.Available)
}

func (e *PlatformMismatchError) Unwrap() error { return e.Err }
