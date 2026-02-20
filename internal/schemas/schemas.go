package schemas

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
)

const RootConfigSchemaID = "https://schemas.tally.dev/root/tally-config.schema.json"

var ruleSchemaIDs = map[string]string{
	"tally/max-lines":                    "https://schemas.tally.dev/rules/tally/max_lines.schema.json",
	"tally/consistent-indentation":       "https://schemas.tally.dev/rules/tally/consistent_indentation.schema.json",
	"tally/no-trailing-spaces":           "https://schemas.tally.dev/rules/tally/no_trailing_spaces.schema.json",
	"tally/newline-between-instructions": "https://schemas.tally.dev/rules/tally/newline_between_instructions.schema.json",
	"tally/newline-per-chained-call":     "https://schemas.tally.dev/rules/tally/newline_per_chained_call.schema.json",
	"tally/prefer-add-unpack":            "https://schemas.tally.dev/rules/tally/prefer_add_unpack.schema.json",
	"tally/prefer-run-heredoc":           "https://schemas.tally.dev/rules/tally/prefer_run_heredoc.schema.json",
	"tally/prefer-copy-heredoc":          "https://schemas.tally.dev/rules/tally/prefer_copy_heredoc.schema.json",
	"tally/prefer-multi-stage-build":     "https://schemas.tally.dev/rules/tally/prefer_multi_stage_build.schema.json",
	"hadolint/DL3001":                    "https://schemas.tally.dev/rules/hadolint/dl3001.schema.json",
	"hadolint/DL3026":                    "https://schemas.tally.dev/rules/hadolint/dl3026.schema.json",
}

var schemaFilesByID = map[string]string{
	RootConfigSchemaID: "root/tally-config.schema.json",

	"https://schemas.tally.dev/rules/tally/max_lines.schema.json":                    "rules/tally/max_lines.schema.json",
	"https://schemas.tally.dev/rules/tally/consistent_indentation.schema.json":       "rules/tally/consistent_indentation.schema.json",
	"https://schemas.tally.dev/rules/tally/no_trailing_spaces.schema.json":           "rules/tally/no_trailing_spaces.schema.json",
	"https://schemas.tally.dev/rules/tally/newline_between_instructions.schema.json": "rules/tally/newline_between_instructions.schema.json",
	"https://schemas.tally.dev/rules/tally/newline_per_chained_call.schema.json":     "rules/tally/newline_per_chained_call.schema.json",
	"https://schemas.tally.dev/rules/tally/prefer_add_unpack.schema.json":            "rules/tally/prefer_add_unpack.schema.json",
	"https://schemas.tally.dev/rules/tally/prefer_run_heredoc.schema.json":           "rules/tally/prefer_run_heredoc.schema.json",
	"https://schemas.tally.dev/rules/tally/prefer_copy_heredoc.schema.json":          "rules/tally/prefer_copy_heredoc.schema.json",
	"https://schemas.tally.dev/rules/tally/prefer_multi_stage_build.schema.json":     "rules/tally/prefer_multi_stage_build.schema.json",
	"https://schemas.tally.dev/rules/hadolint/dl3001.schema.json":                    "rules/hadolint/dl3001.schema.json",
	"https://schemas.tally.dev/rules/hadolint/dl3026.schema.json":                    "rules/hadolint/dl3026.schema.json",
}

//go:embed root/*.json rules/*/*.json
var schemasFS embed.FS

func RuleSchemaID(ruleCode string) (string, bool) {
	schemaID, ok := ruleSchemaIDs[ruleCode]
	return schemaID, ok
}

func RuleSchemaIDs() map[string]string {
	out := make(map[string]string, len(ruleSchemaIDs))
	maps.Copy(out, ruleSchemaIDs)
	return out
}

func SchemaFileByID(schemaID string) (string, bool) {
	path, ok := schemaFilesByID[schemaID]
	return path, ok
}

func AllSchemaIDs() []string {
	ids := make([]string, 0, len(schemaFilesByID))
	for schemaID := range schemaFilesByID {
		ids = append(ids, schemaID)
	}
	return ids
}

func ReadSchemaByID(schemaID string) ([]byte, error) {
	path, ok := SchemaFileByID(schemaID)
	if !ok {
		return nil, fmt.Errorf("unknown schema ID %q", schemaID)
	}
	return fs.ReadFile(schemasFS, path)
}
