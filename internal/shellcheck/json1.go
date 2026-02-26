package shellcheck

// JSON1Output is ShellCheck's -f json1 output schema.
// It is a JSON object with a single "comments" array.
type JSON1Output struct {
	Comments []Comment `json:"comments"`
}

type Comment struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	EndLine   int    `json:"endLine"`
	Column    int    `json:"column"`
	EndColumn int    `json:"endColumn"`
	Level     string `json:"level"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Fix       *Fix   `json:"fix"`
}

type Fix struct {
	Replacements []Replacement `json:"replacements"`
}

type Replacement struct {
	Line           int    `json:"line"`
	EndLine        int    `json:"endLine"`
	Column         int    `json:"column"`
	EndColumn      int    `json:"endColumn"`
	InsertionPoint string `json:"insertionPoint"`
	Precedence     int    `json:"precedence"`
	Replacement    string `json:"replacement"`
}
