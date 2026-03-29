package shell

import "path"

// DockerCommandNames extracts command names from CMD/ENTRYPOINT arguments.
//
// Shell form delegates to CommandNamesWithVariant so wrappers like "exec" or
// "env" are handled consistently with RUN parsing. Exec form returns the base
// executable name from argv[0].
func DockerCommandNames(cmdLine []string, prependShell bool, variant Variant) []string {
	if len(cmdLine) == 0 {
		return nil
	}
	if prependShell {
		return CommandNamesWithVariant(cmdLine[0], variant)
	}

	name := path.Base(cmdLine[0])
	if name == "" || name == "." {
		return nil
	}

	return []string{name}
}
