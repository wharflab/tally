# Reporters and Output Formatting

**Research Focus:** Reporter API design, multiple format support, and Go libraries

Based on analysis of golangci-lint, ruff, oxlint, and staticcheck.

---

## Core Reporter Pattern

### Interface Design

**Minimal, flexible interface:**

```go
package reporter

// Reporter formats and outputs lint violations
type Reporter interface {
    Report(violations []Violation) error
}

// Violation represents a single lint issue
type Violation struct {
    // Identity
    RuleCode    string    // e.g., "DL3006"
    RuleName    string    // e.g., "Unpinned base image"

    // Location
    File        string
    Line        int
    Column      int
    EndLine     int       // Optional: for multi-line violations
    EndColumn   int

    // Content
    Message     string
    Detail      string    // Optional: additional context
    Severity    Severity

    // Context
    SourceCode  string    // Optional: code snippet
    DocURL      string    // Optional: link to documentation

    // Fixes
    SuggestedFix *Fix     // Optional: auto-fix suggestion
}

type Severity int

const (
    SeverityError Severity = iota
    SeverityWarning
    SeverityInfo
    SeverityStyle
)

type Fix struct {
    Description string
    Edits       []Edit
}

type Edit struct {
    StartLine   int
    StartColumn int
    EndLine     int
    EndColumn   int
    NewText     string
}
```

---

## Standard Output Formats

### 1. Text Format (Human-Readable)

**golangci-lint style:**

```text
Dockerfile:5:1: Stage name 'MyStage' should be lowercase (stage-name-casing)
FROM alpine:latest AS MyStage
^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Dockerfile:12:5: Use JSON array format for CMD (json-args-recommended)
CMD npm start
    ^~~~~~~~~
```

**With colors:**

```go
package reporter

import (
    "fmt"
    "io"
    "github.com/charmbracelet/lipgloss"
)

type TextReporter struct {
    writer     io.Writer
    useColor   bool
    showSource bool
}

func NewText(w io.Writer, opts ...TextOption) *TextReporter {
    r := &TextReporter{
        writer:     w,
        useColor:   true,
        showSource: true,
    }
    for _, opt := range opts {
        opt(r)
    }
    return r
}

func (r *TextReporter) Report(violations []Violation) error {
    for _, v := range violations {
        r.printViolation(v)
    }
    return nil
}

func (r *TextReporter) printViolation(v Violation) {
    // Format location
    location := fmt.Sprintf("%s:%d:%d:", v.File, v.Line, v.Column)

    // Color by severity
    var severityStyle lipgloss.Style
    if r.useColor {
        severityStyle = r.styleForSeverity(v.Severity)
    }

    // Print: Dockerfile:5:1: message (rule-code)
    fmt.Fprintf(r.writer, "%s %s (%s)\n",
        location,
        severityStyle.Render(v.Message),
        v.RuleCode,
    )

    // Optionally print source code snippet
    if r.showSource && v.SourceCode != "" {
        r.printSourceSnippet(v)
    }

    // Optionally print doc URL
    if v.DocURL != "" {
        fmt.Fprintf(r.writer, "  See: %s\n", v.DocURL)
    }

    fmt.Fprintln(r.writer)
}

func (r *TextReporter) styleForSeverity(s Severity) lipgloss.Style {
    switch s {
    case SeverityError:
        return lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // Red
    case SeverityWarning:
        return lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow
    case SeverityInfo:
        return lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // Blue
    default:
        return lipgloss.NewStyle()
    }
}

func (r *TextReporter) printSourceSnippet(v Violation) {
    lines := strings.Split(v.SourceCode, "\n")
    for i, line := range lines {
        lineNo := v.Line + i
        prefix := fmt.Sprintf("%4d | ", lineNo)

        if i == 0 {
            // Highlight the problematic line
            fmt.Fprintf(r.writer, "%s%s\n",
                lipgloss.NewStyle().Bold(true).Render(prefix),
                line,
            )
            // Add caret indicator
            fmt.Fprintf(r.writer, "%s%s\n",
                strings.Repeat(" ", len(prefix) + v.Column - 1),
                lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("^"),
            )
        } else {
            fmt.Fprintf(r.writer, "%s%s\n", prefix, line)
        }
    }
}
```

### 2. JSON Format (Machine-Readable)

**Structured output for tooling:**

```go
type JSONReporter struct {
    writer io.Writer
    pretty bool
}

func (r *JSONReporter) Report(violations []Violation) error {
    output := struct {
        Violations []Violation `json:"violations"`
        Summary    Summary     `json:"summary"`
    }{
        Violations: violations,
        Summary:    calculateSummary(violations),
    }

    enc := json.NewEncoder(r.writer)
    if r.pretty {
        enc.SetIndent("", "  ")
    }
    return enc.Encode(output)
}

type Summary struct {
    Total    int `json:"total"`
    Errors   int `json:"errors"`
    Warnings int `json:"warnings"`
    Files    int `json:"files"`
}
```

**Example output:**

```json
{
  "violations": [
    {
      "ruleCode": "DL3006",
      "ruleName": "Unpinned base image",
      "file": "Dockerfile",
      "line": 1,
      "column": 1,
      "message": "Always tag the version of an image explicitly",
      "severity": "warning",
      "docURL": "https://docs.tally.dev/rules/DL3006"
    }
  ],
  "summary": {
    "total": 1,
    "errors": 0,
    "warnings": 1,
    "files": 1
  }
}
```

### 3. SARIF Format (CI/CD Integration)

**Static Analysis Results Interchange Format:**

```go
import "github.com/owenrumney/go-sarif/v2/sarif"

type SARIFReporter struct {
    writer io.Writer
    toolVersion string
}

func (r *SARIFReporter) Report(violations []Violation) error {
    report, err := sarif.New(sarif.Version210)
    if err != nil {
        return err
    }

    run := sarif.NewRunWithInformationURI("tally", r.toolVersion,
        "https://github.com/tinovyatkin/tally")

    for _, v := range violations {
        // Convert violation to SARIF result
        result := sarif.NewRuleResult(v.RuleCode).
            WithMessage(sarif.NewTextMessage(v.Message)).
            WithLevel(sarifLevel(v.Severity))

        // Add location
        location := sarif.NewPhysicalLocation().
            WithArtifactLocation(sarif.NewSimpleArtifactLocation(v.File)).
            WithRegion(sarif.NewRegion().
                WithStartLine(v.Line).
                WithStartColumn(v.Column))

        result.AddLocation(sarif.NewLocation().WithPhysicalLocation(location))

        run.AddResult(result)

        // Add rule metadata
        run.Tool.Driver.AddRule(sarif.NewRule(v.RuleCode).
            WithName(v.RuleName).
            WithShortDescription(sarif.NewMultiformatMessageString(v.Message)).
            WithHelpURI(v.DocURL))
    }

    report.AddRun(run)
    return report.PrettyWrite(r.writer)
}

func sarifLevel(s Severity) string {
    switch s {
    case SeverityError:
        return "error"
    case SeverityWarning:
        return "warning"
    default:
        return "note"
    }
}
```

### 4. GitHub Actions Format

**Native GitHub annotations:**

```go
type GitHubActionsReporter struct {
    writer io.Writer
}

func (r *GitHubActionsReporter) Report(violations []Violation) error {
    for _, v := range violations {
        // ::error file=Dockerfile,line=5,col=1::Stage name should be lowercase
        level := "warning"
        if v.Severity == SeverityError {
            level = "error"
        }

        fmt.Fprintf(r.writer, "::%s file=%s,line=%d,col=%d::%s\n",
            level, v.File, v.Line, v.Column, v.Message)
    }
    return nil
}
```

---

## Multiple Output Support

### Factory Pattern

```go
package reporter

type Config struct {
    Formats []Format
}

type Format struct {
    Type string  // "text", "json", "sarif", "github-actions"
    Path string  // "stdout", "stderr", or file path
}

func New(format Format) (Reporter, error) {
    writer, err := getWriter(format.Path)
    if err != nil {
        return nil, err
    }

    switch format.Type {
    case "text", "colored-text":
        return NewText(writer, WithColor(format.Type == "colored-text")), nil
    case "json":
        return NewJSON(writer, WithPretty(true)), nil
    case "sarif":
        return NewSARIF(writer), nil
    case "github-actions":
        return NewGitHubActions(writer), nil
    default:
        return nil, fmt.Errorf("unknown format: %s", format.Type)
    }
}

func getWriter(path string) (io.Writer, error) {
    switch path {
    case "stdout", "":
        return os.Stdout, nil
    case "stderr":
        return os.Stderr, nil
    default:
        return os.Create(path)
    }
}
```

### Multiple Simultaneous Outputs

```go
type MultiReporter struct {
    reporters []Reporter
}

func NewMulti(reporters ...Reporter) *MultiReporter {
    return &MultiReporter{reporters: reporters}
}

func (m *MultiReporter) Report(violations []Violation) error {
    for _, r := range m.reporters {
        if err := r.Report(violations); err != nil {
            return fmt.Errorf("reporter failed: %w", err)
        }
    }
    return nil
}

// Usage
reporter := NewMulti(
    NewText(os.Stdout, WithColor(true)),
    NewJSON(jsonFile, WithPretty(true)),
    NewSARIF(sarifFile),
)
```

---

## Recommended Libraries

### 1. Terminal Output: Charmbracelet Lip Gloss ⭐

**Package:** `github.com/charmbracelet/lipgloss`

**Pros:**

- CSS-like styling for terminal output
- Adaptive colors (light/dark background detection)
- Layout primitives (borders, padding, alignment)
- Active development, well-maintained
- Used by many popular Go CLI tools

**Example:**

```go
var (
    errorStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("9")).
        PaddingLeft(1)

    warningStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("11"))

    fileStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("12")).
        Underline(true)
)

func formatViolation(v Violation) string {
    file := fileStyle.Render(fmt.Sprintf("%s:%d:%d", v.File, v.Line, v.Column))
    message := errorStyle.Render(v.Message)
    return fmt.Sprintf("%s %s", file, message)
}
```

### 2. SARIF: go-sarif ⭐

**Package:** `github.com/owenrumney/go-sarif/v2`

**Pros:**

- Full SARIF 2.1.0 support
- Builder pattern API
- Used by tfsec, gosec
- Active maintenance

### 3. Progress Indicators: charmbracelet/bubbles

**Package:** `github.com/charmbracelet/bubbles`

**For future enhancements (real-time linting feedback):**

- Spinners
- Progress bars
- Interactive selection

---

## Advanced Features

### 1. Summary Statistics

```go
type SummaryReporter struct {
    inner Reporter
}

func (s *SummaryReporter) Report(violations []Violation) error {
    // Report violations
    if err := s.inner.Report(violations); err != nil {
        return err
    }

    // Print summary
    summary := calculateSummary(violations)
    fmt.Printf("\n")
    fmt.Printf("Found %d issues:\n", summary.Total)
    fmt.Printf("  %d errors\n", summary.Errors)
    fmt.Printf("  %d warnings\n", summary.Warnings)
    fmt.Printf("  %d info\n", summary.Info)
    fmt.Printf("Checked %d files\n", summary.Files)

    return nil
}
```

### 2. Grouped Output

**Group violations by file:**

```go
type GroupedReporter struct {
    inner Reporter
}

func (g *GroupedReporter) Report(violations []Violation) error {
    // Group by file
    byFile := make(map[string][]Violation)
    for _, v := range violations {
        byFile[v.File] = append(byFile[v.File], v)
    }

    // Report each file's violations
    for file, violations := range byFile {
        fmt.Printf("\n%s:\n", file)
        for _, v := range violations {
            fmt.Printf("  %d:%d %s (%s)\n",
                v.Line, v.Column, v.Message, v.RuleCode)
        }
    }

    return nil
}
```

### 3. Filtering and Sorting

```go
type FilteredReporter struct {
    inner       Reporter
    minSeverity Severity
}

func (f *FilteredReporter) Report(violations []Violation) error {
    filtered := make([]Violation, 0, len(violations))
    for _, v := range violations {
        if v.Severity >= f.minSeverity {
            filtered = append(filtered, v)
        }
    }

    // Sort by severity, then file, then line
    sort.Slice(filtered, func(i, j int) bool {
        if filtered[i].Severity != filtered[j].Severity {
            return filtered[i].Severity > filtered[j].Severity  // Errors first
        }
        if filtered[i].File != filtered[j].File {
            return filtered[i].File < filtered[j].File
        }
        return filtered[i].Line < filtered[j].Line
    })

    return f.inner.Report(filtered)
}
```

---

## Configuration

### Output Configuration

```toml
# .tally.toml

[[output]]
format = "text"
path = "stdout"
color = true
show-source = true

[[output]]
format = "json"
path = "tally-report.json"
pretty = true

[[output]]
format = "sarif"
path = "tally-report.sarif"
```

### CLI Flags

```go
// cmd/tally/cmd/check.go
&cli.StringFlag{
    Name:    "format",
    Aliases: []string{"f"},
    Usage:   "Output format: text, json, sarif, github-actions",
    Value:   "text",
},
&cli.StringFlag{
    Name:  "output",
    Aliases: []string{"o"},
    Usage: "Output path (stdout, stderr, or file path)",
    Value: "stdout",
},
&cli.BoolFlag{
    Name:  "no-color",
    Usage: "Disable colored output",
},
```

---

## Implementation Checklist

### Phase 1: Core Formats

- [ ] Reporter interface
- [ ] Violation struct with all fields
- [ ] Text reporter with lipgloss
- [ ] JSON reporter
- [ ] Factory pattern for format selection
- [ ] Summary statistics

### Phase 2: CI/CD Integration

- [ ] SARIF reporter (go-sarif library)
- [ ] GitHub Actions reporter
- [ ] Multi-reporter support
- [ ] Exit codes based on violations

### Phase 3: Advanced Features

- [ ] Grouped output (by file/rule)
- [ ] Filtered output (by severity)
- [ ] Source code snippets
- [ ] Documentation links
- [ ] Progress indicators (for multiple files)

---

## Testing Strategy

```go
func TestReporters(t *testing.T) {
    violations := []Violation{
        {
            RuleCode: "DL3006",
            File:     "Dockerfile",
            Line:     1,
            Column:   1,
            Message:  "Always tag the version explicitly",
            Severity: SeverityWarning,
        },
    }

    tests := []struct {
        name     string
        reporter Reporter
        wantErr  bool
    }{
        {
            name:     "text reporter",
            reporter: NewText(new(bytes.Buffer)),
        },
        {
            name:     "json reporter",
            reporter: NewJSON(new(bytes.Buffer)),
        },
        {
            name:     "sarif reporter",
            reporter: NewSARIF(new(bytes.Buffer)),
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.reporter.Report(violations)
            if (err != nil) != tt.wantErr {
                t.Errorf("Report() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

---

## Key Takeaways

1. ✅ **Simple interface** - `Report([]Violation) error`
2. ✅ **Use Lip Gloss** for terminal styling (lightweight, powerful)
3. ✅ **Use go-sarif** for SARIF output (standard, maintained)
4. ✅ **Factory pattern** for format selection
5. ✅ **Multi-reporter** for simultaneous outputs
6. ✅ **Decorator pattern** for filtering, sorting, grouping
7. 🔄 **Progress indicators** can be added later if needed

---

## References

- golangci-lint printers: `pkg/printers/`
- Lip Gloss: <https://github.com/charmbracelet/lipgloss>
- go-sarif: <https://github.com/owenrumney/go-sarif>
- SARIF spec: <https://docs.oasis-open.org/sarif/sarif/v2.1.0/>
- GitHub Actions: <https://docs.github.com/actions/using-workflows/workflow-commands-for-github-actions>
