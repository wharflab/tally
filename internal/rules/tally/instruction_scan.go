package tally

import (
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/shell"
)

// hasFollowingRunTargetingPath checks if any RUN instruction after afterCmdIdx
// in the stage contains a command named cmdName whose file arguments cover
// targetPath (exact match or parent directory).
//
// This is used for suppression logic: e.g. suppress a "missing --chown" warning
// when a subsequent RUN chown already manages ownership of that path.
func hasFollowingRunTargetingPath(
	stage instructions.Stage,
	afterCmdIdx int,
	cmdName string,
	targetPath string,
	variant shell.Variant,
) bool {
	if targetPath == "" {
		return false
	}

	for i := afterCmdIdx + 1; i < len(stage.Commands); i++ {
		run, ok := stage.Commands[i].(*instructions.RunCommand)
		if !ok {
			continue
		}

		script := getRunCmdLine(run)
		if script == "" {
			continue
		}

		for _, cmd := range shell.FindCommands(script, variant, cmdName) {
			if cmdFileArgCoversPath(&cmd, targetPath) {
				return true
			}
		}
	}

	return false
}

// cmdFileArgCoversPath checks if any file argument of a command covers the
// given path. For commands like chown/chmod, the file arguments are all
// non-flag positional arguments after the first non-flag arg (which is the
// mode/owner spec).
//
// A file arg "covers" a path if the path is equal to or a child of the arg:
//
//	/app covers /app, /app/file, /app/sub/file
//	/etc/app covers /etc/app/config.conf
func cmdFileArgCoversPath(cmd *shell.CommandInfo, targetPath string) bool {
	sawOwnerOrMode := false // first positional arg is owner (chown) or mode (chmod) spec
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !sawOwnerOrMode {
			sawOwnerOrMode = true
			continue
		}

		filePath := path.Clean(arg)
		if !path.IsAbs(filePath) {
			continue // skip relative paths — can't reliably match
		}

		if filePath == targetPath || strings.HasPrefix(targetPath, filePath+"/") {
			return true
		}
	}

	return false
}
