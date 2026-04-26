// Package invocation models planned Dockerfile build invocations produced by
// Dockerfile-oriented entrypoints and build orchestrators such as Bake and
// Compose.
package invocation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"
)

const (
	defaultDockerfileName = "Dockerfile"

	KindDockerfile = "dockerfile"
	KindBake       = "bake"
	KindCompose    = "compose"

	ContextKindDir         = "dir"
	ContextKindTar         = "tar"
	ContextKindGit         = "git"
	ContextKindURL         = "url"
	ContextKindEmpty       = "empty"
	ContextKindDockerImage = "docker-image"
	ContextKindTarget      = "target"
	ContextKindService     = "service"
	ContextKindOCILayout   = "oci-layout"

	SecretScopeBuild   = "build"
	SecretScopeService = "service"
)

// BuildInvocation describes one planned build of one Dockerfile under one
// invocation context. Providers must store local path-typed fields as absolute,
// cleaned paths at runtime, including DockerfilePath, ContextRef.Value when the
// context kind is "dir", and NamedContexts values that refer to local dirs. The
// rest of the pipeline relies on simple string equality for grouping and lookup.
type BuildInvocation struct {
	Key string `json:"-"`

	Source InvocationSource `json:"source"`

	DockerfilePath string     `json:"dockerfilePath"`
	ContextRef     ContextRef `json:"context"`

	BuildArgs     map[string]*string    `json:"buildArgs,omitempty"`
	Platforms     []string              `json:"platforms,omitempty"`
	TargetStage   string                `json:"targetStage,omitempty"`
	NamedContexts map[string]ContextRef `json:"namedContexts,omitempty"`

	Environment    map[string]*string `json:"environment,omitempty"`
	PublishedPorts []PortBinding      `json:"publishedPorts,omitempty"`
	ExposedPorts   []PortSpec         `json:"exposedPorts,omitempty"`
	Networks       []string           `json:"networks,omitempty"`
	// Labels flattens build-time image labels and Compose service/container
	// labels into one map. Compose service labels take precedence over build
	// labels with the same key, so consumers must not assume every label came
	// from a Dockerfile LABEL instruction.
	Labels             map[string]string `json:"labels,omitempty"`
	Secrets            []SecretRef       `json:"secrets,omitempty"`
	Healthcheck        *HealthcheckSpec  `json:"healthcheck,omitempty"`
	EntrypointOverride *CommandOverride  `json:"entrypointOverride,omitempty"`
	CommandOverride    *CommandOverride  `json:"commandOverride,omitempty"`
	RuntimeUser        string            `json:"runtimeUser,omitempty"`
	RuntimeWorkingDir  string            `json:"runtimeWorkingDir,omitempty"`
	StopSignal         string            `json:"stopSignal,omitempty"`
}

// InvocationSource identifies the source declaration that produced an
// invocation.
type InvocationSource struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Name string `json:"name,omitempty"`
}

// ContextRef classifies a declared build context without dereferencing remote
// content.
type ContextRef struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

// PortSpec models container-visible ports such as Compose expose entries.
type PortSpec struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Protocol string `json:"protocol"`
}

// PortBinding models published host-to-container ports such as Compose ports.
type PortBinding struct {
	ContainerStart int    `json:"containerStart"`
	ContainerEnd   int    `json:"containerEnd"`
	HostStart      int    `json:"hostStart,omitempty"`
	HostEnd        int    `json:"hostEnd,omitempty"`
	HostIP         string `json:"hostIP,omitempty"`
	Protocol       string `json:"protocol"`
	AppProtocol    string `json:"appProtocol,omitempty"`
	Mode           string `json:"mode,omitempty"`
}

// SecretRef stores declaration metadata only. It never stores secret values.
// Source may be an absolute local file path, an env:<name> environment source,
// or a provider-specific secret identifier.
type SecretRef struct {
	Scope  string `json:"scope"`
	ID     string `json:"id,omitempty"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
}

func envSecretSource(name string) string {
	if name == "" {
		return ""
	}
	return "env:" + name
}

// HealthcheckSpec preserves Compose healthcheck override metadata.
type HealthcheckSpec struct {
	Test          []string `json:"test,omitempty"`
	Disable       bool     `json:"disable,omitempty"`
	Interval      string   `json:"interval,omitempty"`
	Timeout       string   `json:"timeout,omitempty"`
	Retries       *uint64  `json:"retries,omitempty"`
	StartPeriod   string   `json:"startPeriod,omitempty"`
	StartInterval string   `json:"startInterval,omitempty"`

	IntervalDur      time.Duration `json:"-"`
	TimeoutDur       time.Duration `json:"-"`
	StartPeriodDur   time.Duration `json:"-"`
	StartIntervalDur time.Duration `json:"-"`
}

// UnmarshalJSON preserves the string form while also populating parsed duration
// companions for consumers that need numeric comparisons.
func (s *HealthcheckSpec) UnmarshalJSON(data []byte) error {
	type healthcheckSpec HealthcheckSpec
	var parsed healthcheckSpec
	if err := jsonv2.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*s = HealthcheckSpec(parsed)
	return s.parseDurations()
}

func (s *HealthcheckSpec) parseDurations() error {
	var errs []error
	parse := func(name, raw string, dst *time.Duration) {
		if raw == "" {
			return
		}
		duration, err := time.ParseDuration(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", name, raw, err))
			return
		}
		*dst = duration
	}
	parse("interval", s.Interval, &s.IntervalDur)
	parse("timeout", s.Timeout, &s.TimeoutDur)
	parse("startPeriod", s.StartPeriod, &s.StartPeriodDur)
	parse("startInterval", s.StartInterval, &s.StartIntervalDur)
	return errors.Join(errs...)
}

// CommandOverride preserves Compose command/entrypoint override semantics.
type CommandOverride struct {
	Args             []string `json:"args,omitempty"`
	ClearsImageValue bool     `json:"clearsImageValue,omitempty"`
}

// ResolveOptions configures provider discovery.
type ResolveOptions struct {
	Path     string
	Targets  []string
	Services []string
}

// DiscoveryResult is the normalized output of provider discovery.
type DiscoveryResult struct {
	Kind               string
	EntrypointPath     string
	Invocations        []BuildInvocation
	ZeroLintableReason string
}

// Provider discovers invocations from an orchestrator source.
type Provider interface {
	Discover(ctx context.Context, opts ResolveOptions) (*DiscoveryResult, error)
}
