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

func CanParsePowerShellScript(_ string) bool {
	return false
}

func PowerShellAssignment(_ string) (string, string, bool) {
	return "", "", false
}

func analyzePowerShellScript(_ string) *powerShellScriptAnalysis {
	return nil
}

func hasPowerShellFlowControl(_, _ string) bool {
	return false
}
