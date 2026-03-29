package telemetry

// ToolID identifies a telemetry-aware tool in the catalog.
type ToolID string

// Tool describes one official telemetry opt-out supported by the rule.
type Tool struct {
	ID       ToolID
	Name     string
	EnvKey   string
	EnvValue string
}

const (
	ToolBun         ToolID = "bun"
	ToolAzureCLI    ToolID = "azure-cli"
	ToolWrangler    ToolID = "wrangler"
	ToolHuggingFace ToolID = "hugging-face"
	ToolYarnBerry   ToolID = "yarn-berry"
	ToolNextJS      ToolID = "nextjs"
	ToolNuxt        ToolID = "nuxt"
	ToolGatsby      ToolID = "gatsby"
	ToolAstro       ToolID = "astro"
	ToolTurborepo   ToolID = "turborepo"
	ToolDotNetCLI   ToolID = "dotnet-cli"
	ToolPowerShell  ToolID = "powershell"
	ToolVcpkg       ToolID = "vcpkg"
	ToolHomebrew    ToolID = "homebrew"
)

var orderedTools = []Tool{
	{ID: ToolBun, Name: "Bun", EnvKey: "DO_NOT_TRACK", EnvValue: "1"},
	{ID: ToolAzureCLI, Name: "Azure CLI", EnvKey: "AZURE_CORE_COLLECT_TELEMETRY", EnvValue: "0"},
	{ID: ToolWrangler, Name: "Wrangler", EnvKey: "WRANGLER_SEND_METRICS", EnvValue: "false"},
	{ID: ToolHuggingFace, Name: "Hugging Face Python ecosystem", EnvKey: "HF_HUB_DISABLE_TELEMETRY", EnvValue: "1"},
	{ID: ToolYarnBerry, Name: "Yarn Berry", EnvKey: "YARN_ENABLE_TELEMETRY", EnvValue: "0"},
	{ID: ToolNextJS, Name: "Next.js", EnvKey: "NEXT_TELEMETRY_DISABLED", EnvValue: "1"},
	{ID: ToolNuxt, Name: "Nuxt", EnvKey: "NUXT_TELEMETRY_DISABLED", EnvValue: "1"},
	{ID: ToolGatsby, Name: "Gatsby", EnvKey: "GATSBY_TELEMETRY_DISABLED", EnvValue: "1"},
	{ID: ToolAstro, Name: "Astro", EnvKey: "ASTRO_TELEMETRY_DISABLED", EnvValue: "1"},
	{ID: ToolTurborepo, Name: "Turborepo", EnvKey: "TURBO_TELEMETRY_DISABLED", EnvValue: "1"},
	{ID: ToolDotNetCLI, Name: ".NET CLI", EnvKey: "DOTNET_CLI_TELEMETRY_OPTOUT", EnvValue: "1"},
	{ID: ToolPowerShell, Name: "PowerShell", EnvKey: "POWERSHELL_TELEMETRY_OPTOUT", EnvValue: "1"},
	{ID: ToolVcpkg, Name: "vcpkg", EnvKey: "VCPKG_DISABLE_METRICS", EnvValue: "1"},
	{ID: ToolHomebrew, Name: "Homebrew", EnvKey: "HOMEBREW_NO_ANALYTICS", EnvValue: "1"},
}

var toolsByID = func() map[ToolID]Tool {
	out := make(map[ToolID]Tool, len(orderedTools))
	for _, tool := range orderedTools {
		out[tool.ID] = tool
	}
	return out
}()

// OrderedTools returns the catalog in stable insertion order.
func OrderedTools() []Tool {
	return append([]Tool(nil), orderedTools...)
}

// ToolByID looks up a tool by catalog id.
func ToolByID(id ToolID) (Tool, bool) {
	tool, ok := toolsByID[id]
	return tool, ok
}

// OrderedToolIDs returns ids from the provided set using catalog order.
func OrderedToolIDs(set map[ToolID]bool) []ToolID {
	if len(set) == 0 {
		return nil
	}

	ids := make([]ToolID, 0, len(set))
	for _, tool := range orderedTools {
		if set[tool.ID] {
			ids = append(ids, tool.ID)
		}
	}
	return ids
}
