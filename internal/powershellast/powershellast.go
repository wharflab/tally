package powershellast

import (
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/wharflab/tally/internal/powershellast/queries"
)

var language = grammars.PowershellLanguage()

var (
	commandsQueryOnce sync.Once
	commandsQuery     *queries.PowerShellCommandsQuery

	semanticQueryOnce sync.Once
	semanticQuery     *queries.PowerShellSemanticQuery
)

func Language() *gotreesitter.Language {
	return language
}

func CommandsQuery() *queries.PowerShellCommandsQuery {
	commandsQueryOnce.Do(func() {
		if language == nil {
			return
		}
		q, err := queries.NewPowerShellCommandsQuery(language)
		if err == nil {
			commandsQuery = q
		}
	})
	return commandsQuery
}

func SemanticQuery() *queries.PowerShellSemanticQuery {
	semanticQueryOnce.Do(func() {
		if language == nil {
			return
		}
		q, err := queries.NewPowerShellSemanticQuery(language)
		if err == nil {
			semanticQuery = q
		}
	})
	return semanticQuery
}

func Parse(script string) (*gotreesitter.Tree, []byte) {
	if script == "" || language == nil {
		return nil, nil
	}

	source := []byte(script)
	parser := gotreesitter.NewParser(language)
	tree, err := parser.Parse(source)
	if err != nil || tree == nil {
		return nil, source
	}
	return tree, source
}
