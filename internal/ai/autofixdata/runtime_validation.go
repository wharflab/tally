package autofixdata

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
)

// RuntimeSnapshot captures the subset of final-stage instructions that
// AI AutoFix objectives must preserve byte-for-byte in a proposed rewrite
// and that prompt builders need to summarize for the agent.
//
// Pointer fields (Cmd, Entrypoint, User, Workdir, Health) reference the
// last occurrence and drive the equality validators. Count fields record
// how many times the corresponding instruction appears and are used by
// prompt summaries. Per-instruction occurrence slices (AllWorkdirs, …)
// and aggregated key/port slices (Env, Labels, Expose) support both the
// validators and the prompt renderer without re-walking stage.Commands.
type RuntimeSnapshot struct {
	Cmd        *instructions.CmdCommand
	Entrypoint *instructions.EntrypointCommand
	User       *instructions.UserCommand
	Workdir    *instructions.WorkdirCommand
	Health     *instructions.HealthCheckCommand

	// Aggregated equality-validation values (all occurrences flattened).
	Expose []string
	Env    instructions.KeyValuePairs
	Labels instructions.KeyValuePairs

	// Per-instruction occurrence counts.
	CmdCount        int
	EntrypointCount int
	UserCount       int
	WorkdirCount    int
	HealthCount     int
	EnvCount        int
	LabelCount      int
	ExposeCount     int

	// Stringified per-occurrence renderings, used by prompt summaries.
	AllCmds        []string
	AllEntrypoints []string
	AllUsers       []string
	AllWorkdirs    []string
	AllHealths     []string
}

// ExtractFinalStageRuntime returns a RuntimeSnapshot for the final stage of
// parsed. It walks every instruction in the final stage once and captures
// the runtime-relevant ones, ignoring RUN, COPY, ADD, FROM, ARG, etc.
func ExtractFinalStageRuntime(parsed *dockerfile.ParseResult) RuntimeSnapshot {
	if parsed == nil || len(parsed.Stages) == 0 {
		return RuntimeSnapshot{}
	}
	return extractRuntime(parsed.Stages[len(parsed.Stages)-1])
}

func extractRuntime(stage instructions.Stage) RuntimeSnapshot {
	var rt RuntimeSnapshot
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.CmdCommand:
			rt.Cmd = c
			rt.CmdCount++
			rt.AllCmds = append(rt.AllCmds, c.String())
		case *instructions.EntrypointCommand:
			rt.Entrypoint = c
			rt.EntrypointCount++
			rt.AllEntrypoints = append(rt.AllEntrypoints, c.String())
		case *instructions.UserCommand:
			rt.User = c
			rt.UserCount++
			rt.AllUsers = append(rt.AllUsers, c.String())
		case *instructions.ExposeCommand:
			rt.ExposeCount++
			rt.Expose = append(rt.Expose, c.Ports...)
		case *instructions.WorkdirCommand:
			rt.Workdir = c
			rt.WorkdirCount++
			rt.AllWorkdirs = append(rt.AllWorkdirs, c.String())
		case *instructions.EnvCommand:
			rt.EnvCount++
			rt.Env = append(rt.Env, c.Env...)
		case *instructions.LabelCommand:
			rt.LabelCount++
			rt.Labels = append(rt.Labels, c.Labels...)
		case *instructions.HealthCheckCommand:
			rt.Health = c
			rt.HealthCount++
			rt.AllHealths = append(rt.AllHealths, c.String())
		}
	}
	return rt
}

// EnvKeys returns every ENV key captured in rt, preserving declaration order.
func (rt RuntimeSnapshot) EnvKeys() []string {
	keys := make([]string, 0, len(rt.Env))
	for _, kv := range rt.Env {
		keys = append(keys, kv.Key)
	}
	return keys
}

// LabelKeys returns every LABEL key captured in rt, preserving declaration order.
func (rt RuntimeSnapshot) LabelKeys() []string {
	keys := make([]string, 0, len(rt.Labels))
	for _, kv := range rt.Labels {
		keys = append(keys, kv.Key)
	}
	return keys
}

// FinalStageRuntimeErrors compares the final-stage runtime invariants of orig
// and proposed and returns one error per mismatched instruction.
//
// Missing parse results are reported as a single error so callers can convert
// to a blocking issue without inventing new error text.
func FinalStageRuntimeErrors(orig, proposed *dockerfile.ParseResult) []error {
	if orig == nil || proposed == nil {
		return []error{errors.New("missing parse results for runtime validation")}
	}
	if len(orig.Stages) == 0 || len(proposed.Stages) == 0 {
		return []error{errors.New("missing stages for runtime validation")}
	}
	o := ExtractFinalStageRuntime(orig)
	p := ExtractFinalStageRuntime(proposed)

	var errs []error
	if err := validateCmdInvariant(o.Cmd, p.Cmd); err != nil {
		errs = append(errs, err)
	}
	if err := validateEntrypointInvariant(o.Entrypoint, p.Entrypoint); err != nil {
		errs = append(errs, err)
	}
	if err := validateUserInvariant(o.User, p.User); err != nil {
		errs = append(errs, err)
	}
	if err := validateExposeInvariant(o.Expose, p.Expose); err != nil {
		errs = append(errs, err)
	}
	if err := validateWorkdirInvariant(o.Workdir, p.Workdir); err != nil {
		errs = append(errs, err)
	}
	if err := validateEnvInvariant(o.Env, p.Env); err != nil {
		errs = append(errs, err)
	}
	if err := validateLabelsInvariant(o.Labels, p.Labels); err != nil {
		errs = append(errs, err)
	}
	if err := validateHealthcheckInvariant(o.Health, p.Health); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func validateCmdInvariant(orig, proposed *instructions.CmdCommand) error {
	if (orig == nil) != (proposed == nil) {
		if orig == nil {
			return errors.New("proposed Dockerfile added CMD to the final stage")
		}
		return errors.New("proposed Dockerfile dropped CMD from the final stage")
	}
	if orig == nil {
		return nil
	}
	if orig.PrependShell != proposed.PrependShell || !slices.Equal(orig.CmdLine, proposed.CmdLine) {
		return fmt.Errorf(
			"proposed Dockerfile changed CMD in the final stage (want %q, got %q)",
			orig.String(), proposed.String(),
		)
	}
	return nil
}

func validateEntrypointInvariant(orig, proposed *instructions.EntrypointCommand) error {
	if (orig == nil) != (proposed == nil) {
		if orig == nil {
			return errors.New("proposed Dockerfile added ENTRYPOINT to the final stage")
		}
		return errors.New("proposed Dockerfile dropped ENTRYPOINT from the final stage")
	}
	if orig == nil {
		return nil
	}
	if orig.PrependShell != proposed.PrependShell || !slices.Equal(orig.CmdLine, proposed.CmdLine) {
		return fmt.Errorf(
			"proposed Dockerfile changed ENTRYPOINT in the final stage (want %q, got %q)",
			orig.String(), proposed.String(),
		)
	}
	return nil
}

func validateUserInvariant(orig, proposed *instructions.UserCommand) error {
	if (orig == nil) != (proposed == nil) {
		if orig == nil {
			return errors.New("proposed Dockerfile added USER to the final stage")
		}
		return errors.New("proposed Dockerfile dropped USER from the final stage")
	}
	if orig == nil {
		return nil
	}
	if strings.TrimSpace(orig.User) != strings.TrimSpace(proposed.User) {
		return fmt.Errorf(
			"proposed Dockerfile changed USER in the final stage (want %q, got %q)",
			orig.User, proposed.User,
		)
	}
	return nil
}

func validateExposeInvariant(origPorts, proposedPorts []string) error {
	if len(origPorts) == 0 && len(proposedPorts) > 0 {
		return errors.New("proposed Dockerfile added EXPOSE to the final stage")
	}
	if len(origPorts) > 0 && len(proposedPorts) == 0 {
		return errors.New("proposed Dockerfile dropped EXPOSE from the final stage")
	}
	if len(origPorts) == 0 {
		return nil
	}

	oa := slices.Clone(origPorts)
	pa := slices.Clone(proposedPorts)
	slices.Sort(oa)
	slices.Sort(pa)
	if !slices.Equal(oa, pa) {
		return fmt.Errorf("proposed Dockerfile changed EXPOSE in the final stage (want %v, got %v)", oa, pa)
	}
	return nil
}

func validateWorkdirInvariant(orig, proposed *instructions.WorkdirCommand) error {
	if (orig == nil) != (proposed == nil) {
		if orig == nil {
			return errors.New("proposed Dockerfile added WORKDIR to the final stage")
		}
		return errors.New("proposed Dockerfile dropped WORKDIR from the final stage")
	}
	if orig == nil {
		return nil
	}
	if strings.TrimSpace(orig.Path) != strings.TrimSpace(proposed.Path) {
		return fmt.Errorf(
			"proposed Dockerfile changed WORKDIR in the final stage (want %q, got %q)",
			orig.Path, proposed.Path,
		)
	}
	return nil
}

func validateEnvInvariant(orig, proposed instructions.KeyValuePairs) error {
	if equalKeyValuePairs(orig, proposed) {
		return nil
	}
	return fmt.Errorf(
		"proposed Dockerfile changed ENV in the final stage (want %s, got %s)",
		formatKeyValuePairs(orig), formatKeyValuePairs(proposed),
	)
}

func validateLabelsInvariant(orig, proposed instructions.KeyValuePairs) error {
	if equalKeyValuePairs(orig, proposed) {
		return nil
	}
	return fmt.Errorf(
		"proposed Dockerfile changed LABEL in the final stage (want %s, got %s)",
		formatKeyValuePairs(orig), formatKeyValuePairs(proposed),
	)
}

func validateHealthcheckInvariant(orig, proposed *instructions.HealthCheckCommand) error {
	if (orig == nil) != (proposed == nil) {
		if orig == nil {
			return errors.New("proposed Dockerfile added HEALTHCHECK to the final stage")
		}
		return errors.New("proposed Dockerfile dropped HEALTHCHECK from the final stage")
	}
	if orig == nil {
		return nil
	}
	if !reflect.DeepEqual(orig.Health, proposed.Health) {
		return fmt.Errorf(
			"proposed Dockerfile changed HEALTHCHECK in the final stage (want %q, got %q)",
			orig.String(), proposed.String(),
		)
	}
	return nil
}

func equalKeyValuePairs(a, b instructions.KeyValuePairs) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

func formatKeyValuePairs(kvs instructions.KeyValuePairs) string {
	if len(kvs) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		parts = append(parts, kv.Key+"="+kv.Value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
