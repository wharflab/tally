package invocation

import (
	"path/filepath"
	"strings"
)

// InvocationContext is the rule-facing view of one build invocation.
type InvocationContext struct {
	invocation *BuildInvocation
}

// NewContext creates an invocation context from normalized invocation metadata.
func NewContext(inv *BuildInvocation) *InvocationContext {
	if inv == nil {
		return nil
	}
	return &InvocationContext{
		invocation: inv,
	}
}

// NewDockerfileInvocation creates the normalized invocation used by direct
// Dockerfile linting when a context directory is declared, for example via
// --context. ClassifyContextRef receives baseDir "." so CanonicalPath resolves
// the context against the process working directory, not the Dockerfile's
// directory.
func NewDockerfileInvocation(dockerfilePath, contextDir string) (*BuildInvocation, error) {
	ctx, err := ClassifyContextRef(".", contextDir)
	if err != nil {
		return nil, err
	}
	if ctx.Kind == ContextKindDir && ctx.Value != "" {
		ctx.Value, err = filepath.Abs(ctx.Value)
		if err != nil {
			return nil, err
		}
		ctx.Value = filepath.Clean(ctx.Value)
	}

	sourceFile := dockerfilePath
	dockerfile := dockerfilePath
	if sourceFile != "" && !strings.HasPrefix(sourceFile, "<") {
		if canonical, err := CanonicalPath(sourceFile); err == nil {
			sourceFile = canonical
			dockerfile = canonical
		} else if abs, absErr := filepath.Abs(sourceFile); absErr == nil {
			sourceFile = filepath.Clean(abs)
			dockerfile = sourceFile
		}
	}

	source := InvocationSource{
		Kind: KindDockerfile,
		File: sourceFile,
	}
	inv := &BuildInvocation{
		Source:         source,
		DockerfilePath: dockerfile,
		ContextRef:     ctx,
	}
	inv.Key = InvocationKey(source, dockerfile)
	return inv, nil
}

// ContextRef returns the declared primary context reference.
func (c *InvocationContext) ContextRef() ContextRef {
	if c == nil || c.invocation == nil {
		return ContextRef{}
	}
	return c.invocation.ContextRef
}

// Invocation returns the underlying build invocation, if any.
func (c *InvocationContext) Invocation() *BuildInvocation {
	if c == nil {
		return nil
	}
	return c.invocation
}
