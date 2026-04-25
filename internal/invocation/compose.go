package invocation

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// ComposeProvider discovers build invocations from Docker Compose files.
type ComposeProvider struct{}

// Discover implements Provider.
func (p ComposeProvider) Discover(ctx context.Context, opts ResolveOptions) (*DiscoveryResult, error) {
	entrypoint, err := CanonicalPath(opts.Path)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(entrypoint)

	projectOpts, err := composecli.NewProjectOptions(
		[]string{entrypoint},
		composecli.WithWorkingDirectory(baseDir),
		composecli.WithOsEnv,
		composecli.WithEnvFiles(),
		composecli.WithDotEnv,
		composecli.WithProfiles([]string{}),
		composecli.WithResolvedPaths(true),
	)
	if err != nil {
		return nil, fmt.Errorf("load Compose options for %s: %w", entrypoint, err)
	}
	project, err := projectOpts.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Compose file %s: %w", entrypoint, err)
	}

	if err := rejectProfileGatedBuilds(project); err != nil {
		return nil, err
	}

	names, err := composeServiceNames(project, opts.Services)
	if err != nil {
		return nil, err
	}

	result := &DiscoveryResult{
		Kind:           KindCompose,
		EntrypointPath: entrypoint,
		Invocations:    make([]BuildInvocation, 0, len(names)),
	}
	for _, name := range names {
		service := project.Services[name]
		inv, err := composeInvocation(entrypoint, baseDir, name, service, project)
		if err != nil {
			return nil, err
		}
		result.Invocations = append(result.Invocations, inv)
	}
	if len(result.Invocations) == 0 {
		result.ZeroLintableReason = "Compose file has no active services with a build section"
	}
	return result, nil
}

func rejectProfileGatedBuilds(project *composetypes.Project) error {
	if project == nil {
		return nil
	}
	var names []string
	for name, service := range project.DisabledServices {
		if service.Build != nil {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return fmt.Errorf("compose profiles are not supported for buildable services: %s", strings.Join(names, ", "))
}

func composeServiceNames(project *composetypes.Project, requested []string) ([]string, error) {
	if project == nil {
		return nil, nil
	}
	if len(requested) > 0 {
		names := dedupePreserveOrder(requested)
		for _, name := range names {
			service, ok := project.Services[name]
			if !ok {
				if _, disabled := project.DisabledServices[name]; disabled {
					return nil, fmt.Errorf("compose service %q is disabled by profiles, which are not supported", name)
				}
				return nil, fmt.Errorf("unknown Compose service %q", name)
			}
			if service.Build == nil {
				return nil, fmt.Errorf("compose service %q has no build section", name)
			}
		}
		return names, nil
	}

	names := make([]string, 0, len(project.Services))
	for name, service := range project.Services {
		if service.Build != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func composeInvocation(
	entrypoint, baseDir, name string,
	service composetypes.ServiceConfig,
	project *composetypes.Project,
) (BuildInvocation, error) {
	build := service.Build
	if build == nil {
		return BuildInvocation{}, fmt.Errorf("compose service %q has no build section", name)
	}
	if build.DockerfileInline != "" {
		return BuildInvocation{}, fmt.Errorf("compose service %q uses build.dockerfile_inline, which is not supported", name)
	}

	contextValue := build.Context
	if contextValue == "" {
		contextValue = "."
	}
	ctxRef, err := ClassifyContextRef(baseDir, contextValue)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("compose service %q has invalid build context: %w", name, err)
	}
	dockerfilePath, err := ResolveDockerfilePath(baseDir, ctxRef, build.Dockerfile)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("compose service %q: %w", name, err)
	}

	source := InvocationSource{
		Kind: KindCompose,
		File: entrypoint,
		Name: name,
	}
	platforms := cloneStrings(build.Platforms)
	if len(platforms) == 0 && service.Platform != "" {
		platforms = []string{service.Platform}
	}
	namedContexts, err := normalizeNamedContexts(baseDir, build.AdditionalContexts)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("compose service %q has invalid additional context: %w", name, err)
	}
	publishedPorts, err := composePublishedPorts(service.Ports)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("compose service %q has invalid published port: %w", name, err)
	}
	exposedPorts, err := composeExposedPorts(service.Expose)
	if err != nil {
		return BuildInvocation{}, fmt.Errorf("compose service %q has invalid exposed port: %w", name, err)
	}

	inv := BuildInvocation{
		Source:             source,
		DockerfilePath:     dockerfilePath,
		ContextRef:         ctxRef,
		BuildArgs:          cloneMappingWithEquals(build.Args),
		Platforms:          platforms,
		TargetStage:        build.Target,
		NamedContexts:      namedContexts,
		Environment:        cloneMappingWithEquals(service.Environment),
		PublishedPorts:     publishedPorts,
		ExposedPorts:       exposedPorts,
		Networks:           composeNetworkNames(service),
		Labels:             composeLabels(build.Labels, service.Labels),
		Secrets:            composeSecrets(baseDir, build.Secrets, service.Secrets, project.Secrets),
		Healthcheck:        composeHealthcheck(service.HealthCheck),
		EntrypointOverride: composeCommandOverride(service.Entrypoint),
		CommandOverride:    composeCommandOverride(service.Command),
		RuntimeUser:        service.User,
		RuntimeWorkingDir:  service.WorkingDir,
		StopSignal:         service.StopSignal,
	}
	inv.Key = InvocationKey(source, dockerfilePath)
	return inv, nil
}

func cloneMappingWithEquals(in composetypes.MappingWithEquals) map[string]*string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*string, len(in))
	for key, value := range in {
		if value == nil {
			out[key] = nil
			continue
		}
		cp := *value
		out[key] = &cp
	}
	return out
}

// composeLabels flattens build-time image labels and service/container labels.
// Service labels intentionally take precedence over build labels with the same
// key, so callers must not treat the result as Dockerfile-only label metadata.
func composeLabels(buildLabels, serviceLabels composetypes.Labels) map[string]string {
	labels := make(map[string]string, len(buildLabels)+len(serviceLabels))
	maps.Copy(labels, buildLabels)
	maps.Copy(labels, serviceLabels)
	if len(labels) == 0 {
		return nil
	}
	return labels
}

func composePublishedPorts(ports []composetypes.ServicePortConfig) ([]PortBinding, error) {
	if len(ports) == 0 {
		return nil, nil
	}
	out := make([]PortBinding, 0, len(ports))
	for _, port := range ports {
		protocol := strings.ToLower(port.Protocol)
		if protocol == "" {
			protocol = "tcp"
		}
		hostStart, hostEnd := 0, 0
		if port.Published != "" {
			start, end, err := parsePortRange(port.Published)
			if err != nil {
				return nil, fmt.Errorf("published port %q: %w", port.Published, err)
			}
			hostStart, hostEnd = start, end
		}
		target := int(port.Target)
		out = append(out, PortBinding{
			ContainerStart: target,
			ContainerEnd:   target,
			HostStart:      hostStart,
			HostEnd:        hostEnd,
			HostIP:         port.HostIP,
			Protocol:       protocol,
			AppProtocol:    port.AppProtocol,
			Mode:           port.Mode,
		})
	}
	return out, nil
}

func composeExposedPorts(expose composetypes.StringOrNumberList) ([]PortSpec, error) {
	if len(expose) == 0 {
		return nil, nil
	}
	out := make([]PortSpec, 0, len(expose))
	for _, item := range expose {
		spec, err := parseExpose(item)
		if err != nil {
			return nil, fmt.Errorf("expose %q: %w", item, err)
		}
		out = append(out, spec)
	}
	return out, nil
}

func parseExpose(value string) (PortSpec, error) {
	portPart, protocol, hasProtocol := strings.Cut(value, "/")
	if !hasProtocol || protocol == "" {
		protocol = "tcp"
	}
	start, end, err := parsePortRange(portPart)
	if err != nil {
		return PortSpec{}, err
	}
	return PortSpec{
		Start:    start,
		End:      end,
		Protocol: strings.ToLower(protocol),
	}, nil
}

func composeNetworkNames(service composetypes.ServiceConfig) []string {
	if len(service.Networks) == 0 {
		return nil
	}
	names := make([]string, 0, len(service.Networks))
	for name := range service.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func composeSecrets(
	baseDir string,
	buildSecrets []composetypes.ServiceSecretConfig,
	serviceSecrets []composetypes.ServiceSecretConfig,
	projectSecrets composetypes.Secrets,
) []SecretRef {
	out := make([]SecretRef, 0, len(buildSecrets)+len(serviceSecrets))
	for _, secret := range buildSecrets {
		ref := composeSecretRef(baseDir, SecretScopeBuild, secret, projectSecrets)
		out = append(out, ref)
	}
	for _, secret := range serviceSecrets {
		ref := composeSecretRef(baseDir, SecretScopeService, secret, projectSecrets)
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func composeSecretRef(baseDir, scope string, secret composetypes.ServiceSecretConfig, projectSecrets composetypes.Secrets) SecretRef {
	ref := composetypes.FileReferenceConfig(secret)
	id := ref.Source
	target := ref.Target
	if target == "" {
		target = "/run/secrets/" + id
	}

	source := id
	if top, ok := projectSecrets[id]; ok {
		obj := composetypes.FileObjectConfig(top)
		switch {
		case obj.File != "":
			source = obj.File
			if !filepath.IsAbs(source) {
				source = filepath.Clean(filepath.Join(baseDir, source))
			}
		case obj.Environment != "":
			source = obj.Environment
		}
	}
	return SecretRef{
		Scope:  scope,
		ID:     id,
		Source: source,
		Target: target,
	}
}

func composeHealthcheck(hc *composetypes.HealthCheckConfig) *HealthcheckSpec {
	if hc == nil {
		return nil
	}
	spec := &HealthcheckSpec{
		Test:    cloneStrings([]string(hc.Test)),
		Disable: hc.Disable,
		Retries: hc.Retries,
	}
	if hc.Interval != nil {
		spec.Interval = hc.Interval.String()
		spec.IntervalDur = time.Duration(*hc.Interval)
	}
	if hc.Timeout != nil {
		spec.Timeout = hc.Timeout.String()
		spec.TimeoutDur = time.Duration(*hc.Timeout)
	}
	if hc.StartPeriod != nil {
		spec.StartPeriod = hc.StartPeriod.String()
		spec.StartPeriodDur = time.Duration(*hc.StartPeriod)
	}
	if hc.StartInterval != nil {
		spec.StartInterval = hc.StartInterval.String()
		spec.StartIntervalDur = time.Duration(*hc.StartInterval)
	}
	return spec
}

// composeCommandOverride relies on compose-go preserving unset command fields
// as nil ShellCommand values and explicit command: [] values as non-nil empty
// slices, matching Compose override semantics.
func composeCommandOverride(command composetypes.ShellCommand) *CommandOverride {
	if command == nil {
		return nil
	}
	args := slices.Clone([]string(command))
	if args == nil {
		args = []string{}
	}
	return &CommandOverride{
		Args:             args,
		ClearsImageValue: len(command) == 0,
	}
}
