package autofix

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

var fromLineRE = regexp.MustCompile(`(?im)^\s*FROM\b`)

func hasFromInstruction(content []byte) bool {
	return fromLineRE.FindIndex(content) != nil
}

func countFromInstructions(content []byte) int {
	matches := fromLineRE.FindAllIndex(content, -1)
	return len(matches)
}

func validateStageCount(orig, proposed *dockerfile.ParseResult) error {
	if proposed == nil {
		return errors.New("proposed parse result is nil")
	}
	if !hasFromInstruction(proposed.Source) {
		return errors.New("proposed Dockerfile has no FROM instruction")
	}

	// The prefer-multi-stage-build objective triggers only for single-stage inputs.
	// Enforce 2+ stages in the proposal to avoid accepting a "no-op" rewrite.
	if orig != nil && hasFromInstruction(orig.Source) && countFromInstructions(orig.Source) == 1 {
		if countFromInstructions(proposed.Source) < 2 {
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
		}
	}
	return rt
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

	if o.cmd != nil {
		if p.cmd == nil {
			return errors.New("proposed Dockerfile dropped CMD from the final stage")
		}
		if o.cmd.PrependShell != p.cmd.PrependShell || !slices.Equal(o.cmd.CmdLine, p.cmd.CmdLine) {
			return fmt.Errorf("proposed Dockerfile changed CMD in the final stage (want %q, got %q)", o.cmd.String(), p.cmd.String())
		}
	}
	if o.entrypoint != nil {
		if p.entrypoint == nil {
			return errors.New("proposed Dockerfile dropped ENTRYPOINT from the final stage")
		}
		if o.entrypoint.PrependShell != p.entrypoint.PrependShell || !slices.Equal(o.entrypoint.CmdLine, p.entrypoint.CmdLine) {
			return fmt.Errorf(
				"proposed Dockerfile changed ENTRYPOINT in the final stage (want %q, got %q)",
				o.entrypoint.String(),
				p.entrypoint.String(),
			)
		}
	}
	if o.user != nil {
		if p.user == nil {
			return errors.New("proposed Dockerfile dropped USER from the final stage")
		}
		if strings.TrimSpace(o.user.User) != strings.TrimSpace(p.user.User) {
			return fmt.Errorf("proposed Dockerfile changed USER in the final stage (want %q, got %q)", o.user.User, p.user.User)
		}
	}
	if len(o.expose) > 0 {
		if len(p.expose) == 0 {
			return errors.New("proposed Dockerfile dropped EXPOSE from the final stage")
		}
		oa := slices.Clone(o.expose)
		pa := slices.Clone(p.expose)
		sort.Strings(oa)
		sort.Strings(pa)
		if !slices.Equal(oa, pa) {
			return fmt.Errorf("proposed Dockerfile changed EXPOSE in the final stage (want %v, got %v)", oa, pa)
		}
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
