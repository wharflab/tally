//go:build !cgo

package shell

type powerShellStatement struct {
	Text    string
	HasPipe bool
}

type powerShellScriptAnalysis struct {
	Statements []powerShellStatement
	HasComplex bool
}

func powerShellCommandNames(script string) []string {
	return nil
}

func findPowerShellCommands(script string, names ...string) []CommandInfo {
	return nil
}

func canParsePowerShell(_ string) bool {
	return false // No parser available without cgo.
}

func analyzePowerShellScript(_ string) *powerShellScriptAnalysis {
	return nil
}

func hasPowerShellFlowControl(_, _ string) bool {
	return false
}
