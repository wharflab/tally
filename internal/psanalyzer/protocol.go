package psanalyzer

type AnalyzeRequest struct {
	Path             string
	ScriptDefinition string
	Settings         Settings
}

type FormatRequest struct {
	ScriptDefinition string
}

type Settings struct {
	IncludeRules []string                  `json:"includeRules,omitempty"`
	ExcludeRules []string                  `json:"excludeRules,omitempty"`
	Severity     []string                  `json:"severity,omitempty"`
	Rules        map[string]map[string]any `json:"rules,omitempty"`
}

type Diagnostic struct {
	RuleName   string `json:"ruleName"`
	Severity   int    `json:"severity"`
	Line       *int   `json:"line"`
	Column     *int   `json:"column"`
	EndLine    *int   `json:"endLine"`
	EndColumn  *int   `json:"endColumn"`
	Message    string `json:"message"`
	ScriptPath string `json:"scriptPath"`
}

type request struct {
	ID               string    `json:"id,omitempty"`
	Op               string    `json:"op"`
	Path             string    `json:"path,omitempty"`
	ScriptDefinition string    `json:"scriptDefinition,omitempty"`
	Settings         *Settings `json:"settings,omitempty"`
}

type response struct {
	ID          string       `json:"id,omitempty"`
	Ready       bool         `json:"ready,omitempty"`
	Progress    bool         `json:"progress,omitempty"`
	Message     string       `json:"message,omitempty"`
	Version     string       `json:"version,omitempty"`
	PSVersion   string       `json:"ps,omitempty"`
	OK          bool         `json:"ok,omitempty"`
	Error       string       `json:"error,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Formatted   string       `json:"formatted,omitempty"`
}
