package autofix

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

func countFromInstructions(pr *dockerfile.ParseResult) int {
	if pr == nil {
		return 0
	}

	count := 0
	for _, stage := range pr.Stages {
		if strings.TrimSpace(stage.SourceCode) == "" {
			// Parse may synthesize a dummy stage (FROM scratch) to continue linting.
			// Count only real stage definitions from source.
			continue
		}
		count++
	}
	return count
}

func validateStageCount(orig, proposed *dockerfile.ParseResult) error {
	if proposed == nil {
		return errors.New("proposed parse result is nil")
	}

	proposedFrom := countFromInstructions(proposed)
	if proposedFrom == 0 {
		return errors.New("proposed Dockerfile has no FROM instruction")
	}

	// The prefer-multi-stage-build objective triggers only for single-stage inputs.
	// Enforce 2+ stages in the proposal to avoid accepting a "no-op" rewrite.
	if orig != nil && countFromInstructions(orig) == 1 {
		if proposedFrom < 2 {
			return errors.New("proposed Dockerfile still has a single stage (expected 2+ stages)")
		}
	}
	return nil
}

type stageRuntime struct {
	cmd        *instructions.CmdCommand
	entrypoint *instructions.EntrypointCommand
	user       *instructions.UserCommand
	expose     []string
	workdir    *instructions.WorkdirCommand
	env        instructions.KeyValuePairs
	labels     instructions.KeyValuePairs
	health     *instructions.HealthCheckCommand
}

func extractRuntime(stage instructions.Stage) stageRuntime {
	var rt stageRuntime
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.CmdCommand:
			rt.cmd = c
		case *instructions.EntrypointCommand:
			rt.entrypoint = c
		case *instructions.UserCommand:
			rt.user = c
		case *instructions.ExposeCommand:
			rt.expose = append(rt.expose, c.Ports...)
		case *instructions.WorkdirCommand:
			rt.workdir = c
		case *instructions.EnvCommand:
			rt.env = append(rt.env, c.Env...)
		case *instructions.LabelCommand:
			rt.labels = append(rt.labels, c.Labels...)
		case *instructions.HealthCheckCommand:
			rt.health = c
		}
	}
	return rt
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

func validateCmd(orig, proposed *instructions.CmdCommand) error {
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
		return fmt.Errorf("proposed Dockerfile changed CMD in the final stage (want %q, got %q)", orig.String(), proposed.String())
	}
	return nil
}

func validateEntrypoint(orig, proposed *instructions.EntrypointCommand) error {
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
			orig.String(),
			proposed.String(),
		)
	}
	return nil
}

func validateUser(orig, proposed *instructions.UserCommand) error {
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
		return fmt.Errorf("proposed Dockerfile changed USER in the final stage (want %q, got %q)", orig.User, proposed.User)
	}
	return nil
}

func validateExpose(origPorts, proposedPorts []string) error {
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

func validateWorkdir(orig, proposed *instructions.WorkdirCommand) error {
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
		return fmt.Errorf("proposed Dockerfile changed WORKDIR in the final stage (want %q, got %q)", orig.Path, proposed.Path)
	}
	return nil
}

func validateEnv(orig, proposed instructions.KeyValuePairs) error {
	if equalKeyValuePairs(orig, proposed) {
		return nil
	}
	return errors.New("proposed Dockerfile changed ENV in the final stage")
}

func validateLabels(orig, proposed instructions.KeyValuePairs) error {
	if equalKeyValuePairs(orig, proposed) {
		return nil
	}
	return errors.New("proposed Dockerfile changed LABEL in the final stage")
}

func validateHealthcheck(orig, proposed *instructions.HealthCheckCommand) error {
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
		return errors.New("proposed Dockerfile changed HEALTHCHECK in the final stage")
	}
	return nil
}

func validateRuntimeSettings(orig, proposed *dockerfile.ParseResult) error {
	if orig == nil || proposed == nil {
		return errors.New("missing parse results for runtime validation")
	}
	if len(orig.Stages) == 0 || len(proposed.Stages) == 0 {
		return errors.New("missing stages for runtime validation")
	}

	origFinal := orig.Stages[len(orig.Stages)-1]
	propFinal := proposed.Stages[len(proposed.Stages)-1]
	o := extractRuntime(origFinal)
	p := extractRuntime(propFinal)

	if err := validateCmd(o.cmd, p.cmd); err != nil {
		return err
	}
	if err := validateEntrypoint(o.entrypoint, p.entrypoint); err != nil {
		return err
	}
	if err := validateUser(o.user, p.user); err != nil {
		return err
	}
	if err := validateExpose(o.expose, p.expose); err != nil {
		return err
	}
	if err := validateWorkdir(o.workdir, p.workdir); err != nil {
		return err
	}
	if err := validateEnv(o.env, p.env); err != nil {
		return err
	}
	if err := validateLabels(o.labels, p.labels); err != nil {
		return err
	}
	if err := validateHealthcheck(o.health, p.health); err != nil {
		return err
	}

	return nil
}

func wholeFileReplacement(filePath string, original []byte, newText string) rules.TextEdit {
	sm := sourcemap.New(original)
	endLine := sm.LineCount()
	endCol := 0
	if endLine > 0 {
		endCol = len(sm.Line(endLine - 1))
	}
	return rules.TextEdit{
		Location: rules.NewRangeLocation(filePath, 1, 0, endLine, endCol),
		NewText:  newText,
	}
}
