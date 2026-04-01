//go:build !cgo

package shell

type CmdScriptAnalysis struct {
	Commands []CommandInfo

	commandByteRanges [][2]uint
	conditionalOps    []cmdConditionalOp

	HasConditionals       bool
	HasPipes              bool
	HasRedirections       bool
	HasControlFlow        bool
	HasVariableReferences bool
}

type cmdConditionalOp struct {
	Text  string
	Start uint
	End   uint
}

func (a *CmdScriptAnalysis) HasBatchOnlySyntax() bool {
	return true
}

func cmdCommandNames(script string) []string {
	return nil
}

func findCmdCommands(script string, names ...string) []CommandInfo {
	return nil
}

func AnalyzeCmdScript(script string) *CmdScriptAnalysis {
	return nil
}
