//go:build !cgo

package shell

func powerShellCommandNames(script string) []string {
	return nil
}

func findPowerShellCommands(script string, names ...string) []CommandInfo {
	return nil
}

func canParsePowerShell(_ string) bool {
	return false // No parser available without cgo.
}
