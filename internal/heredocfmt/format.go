// Package heredocfmt formats content embedded in Dockerfile heredocs.
package heredocfmt

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	editorconfig "github.com/editorconfig/editorconfig-core-go/v2"
	toml "github.com/pelletier/go-toml/v2"
	yaml "go.yaml.in/yaml/v4"
	ini "gopkg.in/ini.v1"
	"mvdan.cc/sh/v3/syntax"

	"github.com/wharflab/tally/internal/shell"
)

const defaultIndentWidth = 2
const defaultShellMaxLineLength = 100

var errSkipFormat = errors.New("skip formatting")

// Kind identifies a supported heredoc payload format.
type Kind string

const (
	KindJSON Kind = "json"
	KindYAML Kind = "yaml"
	KindTOML Kind = "toml"
	KindXML  Kind = "xml"
	KindINI  Kind = "ini"
)

// Formatter resolves EditorConfig style once per virtual target filename and formats heredoc content.
type Formatter struct {
	dockerfilePath string
	styleCache     map[string]style
}

type style struct {
	indent           string
	indentWidth      int
	maxLineLength    int
	maxLineLengthSet bool
	shellIndent      uint
	binaryNextLine   bool
	caseIndent       bool
	spaceRedirects   bool
	keepPadding      bool
	functionNextLine bool
	simplify         bool
	minify           bool
}

// NewFormatter creates a formatter for heredocs inside dockerfilePath.
func NewFormatter(dockerfilePath string) *Formatter {
	return &Formatter{
		dockerfilePath: dockerfilePath,
		styleCache:     make(map[string]style),
	}
}

// SupportedKind returns the supported format kind for filename's extension.
func SupportedKind(filename string) (Kind, bool) {
	switch strings.ToLower(path.Ext(filepath.ToSlash(filename))) {
	case ".json":
		return KindJSON, true
	case ".yaml", ".yml":
		return KindYAML, true
	case ".toml":
		return KindTOML, true
	case ".xml", ".config", ".xsd", ".wsdl", ".xsl", ".xslt":
		return KindXML, true
	case ".ini":
		return KindINI, true
	default:
		return "", false
	}
}

// FormatTarget formats content according to the target filename extension and EditorConfig style.
func (f *Formatter) FormatTarget(target, content string) (string, Kind, bool, error) {
	kind, ok := SupportedKind(target)
	if !ok {
		return "", "", false, nil
	}

	st, err := f.styleForTarget(target)
	if err != nil {
		return "", "", false, err
	}

	formatted, err := formatContent(kind, content, st)
	if err != nil {
		if errors.Is(err, errSkipFormat) {
			return "", kind, false, nil
		}
		return "", kind, false, err
	}
	return formatted, kind, true, nil
}

// FormatShell formats a RUN heredoc body as a shell script for a mvdan.cc/sh-supported variant.
func (f *Formatter) FormatShell(content string, variant shell.Variant) (string, bool, error) {
	if !variant.SupportsPOSIXShellAST() {
		return "", false, nil
	}

	st, err := f.styleForShell(variant)
	if err != nil {
		return "", false, err
	}

	formatted, err := formatShell(content, variant, st)
	if err != nil {
		if errors.Is(err, errSkipFormat) {
			return "", false, nil
		}
		return "", false, err
	}
	return formatted, true, nil
}

// FormatShellTarget formats a COPY heredoc body as a shell script when the destination or shebang implies one.
func (f *Formatter) FormatShellTarget(target, content string) (string, shell.Variant, bool, error) {
	variant, ok := shellTargetVariant(target, content)
	if !ok {
		return "", shell.VariantUnknown, false, nil
	}

	st, err := f.styleForShellTarget(target, variant)
	if err != nil {
		return "", variant, false, err
	}

	formatted, err := formatShell(content, variant, st)
	if err != nil {
		if errors.Is(err, errSkipFormat) {
			return "", variant, false, nil
		}
		return "", variant, false, err
	}
	return formatted, variant, true, nil
}

func (f *Formatter) styleForTarget(target string) (style, error) {
	virtualName, err := VirtualFilename(f.dockerfilePath, target)
	if err != nil {
		return style{}, err
	}
	if st, ok := f.styleCache[virtualName]; ok {
		return st, nil
	}

	def, err := editorconfig.GetDefinitionForFilename(virtualName)
	if err != nil {
		return style{}, err
	}
	st := styleFromDefinition(def)
	f.styleCache[virtualName] = st
	return st, nil
}

func (f *Formatter) styleForShell(variant shell.Variant) (style, error) {
	virtualName, err := VirtualShellFilename(f.dockerfilePath, variant)
	if err != nil {
		return style{}, err
	}
	if st, ok := f.styleCache[virtualName]; ok {
		return st, nil
	}

	def, err := editorconfig.GetDefinitionForFilename(virtualName)
	if err != nil {
		return style{}, err
	}
	st := shellStyleFromDefinition(def)
	f.styleCache[virtualName] = st
	return st, nil
}

func (f *Formatter) styleForShellTarget(target string, variant shell.Variant) (style, error) {
	virtualName, err := VirtualShellTargetFilename(f.dockerfilePath, target, variant)
	if err != nil {
		return style{}, err
	}
	if st, ok := f.styleCache[virtualName]; ok {
		return st, nil
	}

	def, err := editorconfig.GetDefinitionForFilename(virtualName)
	if err != nil {
		return style{}, err
	}
	st := shellStyleFromDefinition(def)
	f.styleCache[virtualName] = st
	return st, nil
}

// VirtualFilename returns the synthetic filename used for EditorConfig matching.
//
// The file lives next to the Dockerfile so .editorconfig discovery follows the
// repository layout, while the basename comes from the heredoc destination so
// selectors such as *.json, config.yaml, or package.json still match.
func VirtualFilename(dockerfilePath, target string) (string, error) {
	base := targetBasename(target)
	if base == "" {
		if kind, ok := SupportedKind(target); ok {
			base = "heredoc." + string(kind)
		} else {
			base = "heredoc"
		}
	}
	dir, err := dockerfileDir(dockerfilePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.FromSlash(base)), nil
}

// VirtualShellFilename returns the synthetic filename used for RUN heredoc EditorConfig matching.
func VirtualShellFilename(dockerfilePath string, variant shell.Variant) (string, error) {
	dir, err := dockerfileDir(dockerfilePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Dockerfile.heredoc."+shellExtension(variant)), nil
}

// VirtualShellTargetFilename returns the synthetic filename used for COPY shell heredoc EditorConfig matching.
func VirtualShellTargetFilename(dockerfilePath, target string, variant shell.Variant) (string, error) {
	base := targetBasenameAny(target)
	if base == "" {
		return VirtualShellFilename(dockerfilePath, variant)
	}
	dir, err := dockerfileDir(dockerfilePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.FromSlash(base)), nil
}

func shellExtension(variant shell.Variant) string {
	switch variant {
	case shell.VariantPOSIX:
		return "sh"
	case shell.VariantBash:
		return "bash"
	case shell.VariantMksh:
		return "mksh"
	case shell.VariantBats:
		return "bats"
	case shell.VariantZsh:
		return "zsh"
	case shell.VariantPowerShell, shell.VariantCmd, shell.VariantUnknown:
		return "sh"
	}
	return "sh"
}

func targetBasename(target string) string {
	base := targetBasenameAny(target)
	if base == "" {
		return ""
	}
	if _, ok := SupportedKind(base); !ok {
		return ""
	}
	return base
}

func targetBasenameAny(target string) string {
	target = strings.TrimSpace(target)
	target = strings.Trim(target, `"'`)
	target = strings.ReplaceAll(target, `\`, `/`)
	base := path.Base(target)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}

func dockerfileDir(dockerfilePath string) (string, error) {
	if dockerfilePath == "" || dockerfilePath == "-" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return wd, nil
	}
	abs, err := filepath.Abs(dockerfilePath)
	if err != nil {
		return "", err
	}
	return filepath.Dir(abs), nil
}

func styleFromDefinition(def *editorconfig.Definition) style {
	if def == nil {
		return style{
			indent:      strings.Repeat(" ", defaultIndentWidth),
			indentWidth: defaultIndentWidth,
		}
	}

	width := parseIndentWidth(def)
	maxLineLength, maxLineLengthSet := parseMaxLineLength(def)
	st := style{
		maxLineLength:    maxLineLength,
		maxLineLengthSet: maxLineLengthSet,
		binaryNextLine:   rawBool(def, "binary_next_line"),
		caseIndent:       rawBool(def, "switch_case_indent"),
		spaceRedirects:   rawBool(def, "space_redirects"),
		keepPadding:      rawBool(def, "keep_padding"),
		functionNextLine: rawBool(def, "function_next_line"),
		minify:           rawBool(def, "minify"),
		simplify:         rawBool(def, "simplify") || rawBool(def, "minify"),
	}
	if strings.EqualFold(def.IndentStyle, editorconfig.IndentStyleTab) {
		st.indent = "\t"
		st.indentWidth = width
		return st
	}
	st.indent = strings.Repeat(" ", width)
	st.indentWidth = width
	return st
}

func shellStyleFromDefinition(def *editorconfig.Definition) style {
	st := styleFromDefinition(def)
	st.indent = "\t"
	st.indentWidth = 8
	st.shellIndent = 0

	if def != nil && strings.EqualFold(def.IndentStyle, editorconfig.IndentStyleSpaces) {
		width := 8
		if n := parseIndentWidth(def); n > 0 {
			width = n
		}
		st.indent = strings.Repeat(" ", width)
		st.indentWidth = width
		st.shellIndent = uint(width)
	}
	if !st.maxLineLengthSet {
		st.maxLineLength = defaultShellMaxLineLength
	}
	return st
}

func parseIndentWidth(def *editorconfig.Definition) int {
	if def == nil {
		return defaultIndentWidth
	}
	if n, err := strconv.Atoi(def.IndentSize); err == nil && n > 0 {
		return n
	}
	if def.TabWidth > 0 {
		return def.TabWidth
	}
	return defaultIndentWidth
}

func parseMaxLineLength(def *editorconfig.Definition) (int, bool) {
	if def == nil || def.Raw == nil {
		return 0, false
	}
	value := strings.TrimSpace(strings.ToLower(def.Raw["max_line_length"]))
	if value == "" {
		return 0, false
	}
	if value == "off" {
		return 0, true
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func rawBool(def *editorconfig.Definition, key string) bool {
	if def == nil || def.Raw == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(def.Raw[key]), "true")
}

func shellTargetVariant(target, content string) (shell.Variant, bool) {
	firstLine, _, _ := strings.Cut(content, "\n")
	if name, ok := shell.ShellFromShebang(firstLine); ok {
		variant := shell.VariantFromShell(name)
		return variant, variant.SupportsPOSIXShellAST()
	}
	if strings.HasPrefix(firstLine, "#!") {
		return shell.VariantUnknown, false
	}
	if isDotShTarget(target) {
		return shell.VariantPOSIX, true
	}
	return shell.VariantUnknown, false
}

func isDotShTarget(target string) bool {
	target = strings.TrimSpace(target)
	target = strings.Trim(target, `"'`)
	return strings.EqualFold(path.Ext(filepath.ToSlash(target)), ".sh")
}

func formatContent(kind Kind, content string, st style) (string, error) {
	switch kind {
	case KindJSON:
		return formatJSON(content, st)
	case KindYAML:
		return formatYAML(content, st)
	case KindTOML:
		return formatTOML(content, st)
	case KindXML:
		return formatXML(content, st)
	case KindINI:
		return formatINI(content, st)
	default:
		return "", errSkipFormat
	}
}

func formatShell(content string, variant shell.Variant, st style) (string, error) {
	if strings.TrimSpace(content) == "" || !variant.SupportsPOSIXShellAST() {
		return "", errSkipFormat
	}

	prog, err := parseShell(content, variant)
	if err != nil {
		return "", err
	}
	if st.simplify {
		syntax.Simplify(prog)
	}

	formatted, err := printShell(prog, st)
	if err != nil {
		return "", err
	}
	if st.maxLineLength > 0 && hasLineLongerThan(formatted, st.maxLineLength) {
		wrappedProg, err := parseShell(formatted, variant)
		if err != nil {
			return "", errSkipFormat
		}
		if !wrapLongShellCalls(wrappedProg, st) {
			return "", errSkipFormat
		}
		formatted, err = printShell(wrappedProg, st)
		if err != nil {
			return "", err
		}
		if _, err := parseShell(formatted, variant); err != nil {
			return "", errSkipFormat
		}
		if hasLineLongerThan(formatted, st.maxLineLength) {
			return "", errSkipFormat
		}
	}
	return ensureTrailingNewline(formatted), nil
}

func parseShell(content string, variant shell.Variant) (*syntax.File, error) {
	return syntax.NewParser(
		syntax.KeepComments(true),
		syntax.Variant(shellLangVariant(variant)),
	).Parse(strings.NewReader(content), "")
}

func shellLangVariant(variant shell.Variant) syntax.LangVariant {
	switch variant {
	case shell.VariantPOSIX:
		return syntax.LangPOSIX
	case shell.VariantBash:
		return syntax.LangBash
	case shell.VariantMksh:
		return syntax.LangMirBSDKorn
	case shell.VariantBats:
		return syntax.LangBats
	case shell.VariantZsh:
		return syntax.LangZsh
	case shell.VariantPowerShell, shell.VariantCmd, shell.VariantUnknown:
		return syntax.LangBash
	}
	return syntax.LangBash
}

func printShell(prog *syntax.File, st style) (string, error) {
	var buf bytes.Buffer
	if err := syntax.NewPrinter(shellPrinterOptions(st)...).Print(&buf, prog); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func shellPrinterOptions(st style) []syntax.PrinterOption {
	return []syntax.PrinterOption{
		syntax.Indent(st.shellIndent),
		syntax.BinaryNextLine(st.binaryNextLine),
		syntax.SwitchCaseIndent(st.caseIndent),
		syntax.SpaceRedirects(st.spaceRedirects),
		syntax.KeepPadding(st.keepPadding), //nolint:staticcheck // Match shfmt's supported keep_padding EditorConfig option.
		syntax.FunctionNextLine(st.functionNextLine),
		syntax.Minify(st.minify),
	}
}

func hasLineLongerThan(s string, maxLen int) bool {
	if maxLen <= 0 {
		return false
	}
	s = strings.TrimRight(s, "\n")
	for {
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			return len(s) > maxLen
		}
		if idx > maxLen {
			return true
		}
		s = s[idx+1:]
	}
}

func wrapLongShellCalls(prog *syntax.File, st style) bool {
	modified := false
	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if wrapLongShellCall(call, st) {
			modified = true
		}
		return true
	})
	return modified
}

func wrapLongShellCall(call *syntax.CallExpr, st style) bool {
	if call == nil || len(call.Assigns) > 0 || len(call.Args) < 2 || st.maxLineLength <= 0 {
		return false
	}

	words, ok := shellWordTexts(call.Args, st)
	if !ok {
		return false
	}

	lineLen := leadingColumn(call.Args[0]) + len(words[0])
	if !callNeedsWrap(words, lineLen, st.maxLineLength) {
		return false
	}

	breaks := make([]bool, len(words))
	for i := 1; i < len(words); i++ {
		nextLen := lineLen + 1 + len(words[i])
		if nextLen > st.maxLineLength {
			breaks[i] = true
			lineLen = st.indentWidth + len(words[i])
			continue
		}
		lineLen = nextLen
	}

	line := call.Args[0].Pos().Line()
	if line == 0 {
		line = 1
	}
	currentLine := line
	modified := false
	for i, shouldBreak := range breaks {
		if i == 0 {
			continue
		}
		if shouldBreak {
			currentLine++
		}
		if currentLine == line {
			continue
		}
		if setWordLine(call.Args[i], currentLine) {
			modified = true
		}
	}
	return modified
}

func callNeedsWrap(words []string, firstLineLen, maxLen int) bool {
	if firstLineLen > maxLen {
		return true
	}
	lineLen := firstLineLen
	for _, word := range words[1:] {
		lineLen += 1 + len(word)
		if lineLen > maxLen {
			return true
		}
	}
	return false
}

func leadingColumn(word *syntax.Word) int {
	if word == nil {
		return 0
	}
	col := word.Pos().Col()
	if col == 0 {
		return 0
	}
	return int(col - 1)
}

func shellWordTexts(words []*syntax.Word, st style) ([]string, bool) {
	printer := syntax.NewPrinter(shellPrinterOptions(st)...)
	texts := make([]string, 0, len(words))
	for _, word := range words {
		if word == nil {
			return nil, false
		}
		var buf bytes.Buffer
		if err := printer.Print(&buf, word); err != nil {
			return nil, false
		}
		text := buf.String()
		if text == "" || strings.Contains(text, "\n") {
			return nil, false
		}
		texts = append(texts, text)
	}
	return texts, true
}

func setWordLine(word *syntax.Word, line uint) bool {
	if word == nil || len(word.Parts) == 0 {
		return false
	}
	baseCol := word.Pos().Col()
	if baseCol == 0 {
		baseCol = 1
	}
	for _, part := range word.Parts {
		if !setWordPartLine(part, line, baseCol) {
			return false
		}
	}
	return true
}

func setWordPartsLine(parts []syntax.WordPart, line, baseCol uint) bool {
	for _, part := range parts {
		if !setWordPartLine(part, line, baseCol) {
			return false
		}
	}
	return true
}

func setWordPartLine(part syntax.WordPart, line, baseCol uint) bool {
	switch part := part.(type) {
	case *syntax.Lit:
		setLitLine(part, line, baseCol)
	case *syntax.SglQuoted:
		part.Left = withLineRelative(part.Left, line, baseCol)
		part.Right = withLineRelative(part.Right, line, baseCol)
	case *syntax.DblQuoted:
		part.Left = withLineRelative(part.Left, line, baseCol)
		part.Right = withLineRelative(part.Right, line, baseCol)
		return setWordPartsLine(part.Parts, line, baseCol)
	case *syntax.ParamExp:
		part.Dollar = withLineRelative(part.Dollar, line, baseCol)
		part.Rbrace = withLineRelative(part.Rbrace, line, baseCol)
		setLitLine(part.Flags, line, baseCol)
		setLitLine(part.Param, line, baseCol)
		if part.NestedParam != nil && !setWordPartLine(part.NestedParam, line, baseCol) {
			return false
		}
		for _, modifier := range part.Modifiers {
			setLitLine(modifier, line, baseCol)
		}
		if part.Repl != nil {
			if !setNestedWordLine(part.Repl.Orig, line, baseCol) || !setNestedWordLine(part.Repl.With, line, baseCol) {
				return false
			}
		}
		if part.Exp != nil && !setNestedWordLine(part.Exp.Word, line, baseCol) {
			return false
		}
	case *syntax.CmdSubst:
		part.Left = withLineRelative(part.Left, line, baseCol)
		part.Right = withLineRelative(part.Right, line, baseCol)
	case *syntax.ArithmExp:
		part.Left = withLineRelative(part.Left, line, baseCol)
		part.Right = withLineRelative(part.Right, line, baseCol)
	case *syntax.ProcSubst:
		part.OpPos = withLineRelative(part.OpPos, line, baseCol)
		part.Rparen = withLineRelative(part.Rparen, line, baseCol)
	case *syntax.ExtGlob:
		part.OpPos = withLineRelative(part.OpPos, line, baseCol)
		setLitLine(part.Pattern, line, baseCol)
	case *syntax.BraceExp:
		for _, elem := range part.Elems {
			if !setNestedWordLine(elem, line, baseCol) {
				return false
			}
		}
	default:
		return false
	}
	return true
}

func setNestedWordLine(word *syntax.Word, line, baseCol uint) bool {
	if word == nil {
		return true
	}
	return setWordPartsLine(word.Parts, line, baseCol)
}

func setLitLine(lit *syntax.Lit, line, baseCol uint) {
	if lit == nil {
		return
	}
	lit.ValuePos = withLineRelative(lit.ValuePos, line, baseCol)
	lit.ValueEnd = withLineRelative(lit.ValueEnd, line, baseCol)
}

func withLineRelative(pos syntax.Pos, line, baseCol uint) syntax.Pos {
	if !pos.IsValid() {
		return pos
	}
	col := pos.Col()
	if col == 0 {
		col = 1
	} else if col >= baseCol {
		col = col - baseCol + 1
	}
	return syntax.NewPos(0, line, col)
}

func formatJSON(content string, st style) (string, error) {
	src := bytes.TrimSpace([]byte(content))
	if len(src) == 0 {
		return "", errSkipFormat
	}
	out, err := jsontext.AppendFormat(nil, src, jsontext.WithIndentPrefix(""), jsontext.WithIndent(st.indent))
	if err != nil {
		return "", err
	}
	return ensureTrailingNewline(string(out)), nil
}

func formatYAML(content string, st style) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errSkipFormat
	}

	dec := yaml.NewDecoder(strings.NewReader(content))
	var node yaml.Node
	if err := dec.Decode(&node); err != nil {
		return "", err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", errSkipFormat
	}
	if node.Kind == 0 {
		return "", errSkipFormat
	}
	clearYAMLFlowStyle(&node)

	if st.maxLineLength > 0 {
		return formatYAMLWithLineWidth(&node, st)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(st.indentWidth)
	if err := enc.Encode(&node); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return ensureTrailingNewline(strings.TrimSuffix(buf.String(), "...\n")), nil
}

func formatYAMLWithLineWidth(node *yaml.Node, st style) (string, error) {
	var buf bytes.Buffer
	dumper, err := yaml.NewDumper(&buf, yaml.WithIndent(st.indentWidth), yaml.WithLineWidth(st.maxLineLength))
	if err != nil {
		return "", err
	}
	if err := dumper.Dump(node); err != nil {
		_ = dumper.Close()
		return "", err
	}
	if err := dumper.Close(); err != nil {
		return "", err
	}
	return ensureTrailingNewline(strings.TrimSuffix(buf.String(), "...\n")), nil
}

func clearYAMLFlowStyle(node *yaml.Node) {
	if node == nil {
		return
	}
	node.Style &^= yaml.FlowStyle
	for _, child := range node.Content {
		clearYAMLFlowStyle(child)
	}
}

func formatTOML(content string, st style) (string, error) {
	if strings.TrimSpace(content) == "" || tomlHasComment(content) {
		return "", errSkipFormat
	}
	var doc map[string]any
	if err := toml.Unmarshal([]byte(content), &doc); err != nil {
		return "", err
	}
	if len(doc) == 0 {
		return "", errSkipFormat
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf).SetIndentSymbol(st.indent)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	return ensureTrailingNewline(buf.String()), nil
}

func tomlHasComment(content string) bool {
	inString := false
	stringQuote := byte(0)
	multiline := false
	escaped := false
	for i := 0; i < len(content); {
		ch := content[i]
		if inString {
			if multiline && strings.HasPrefix(content[i:], strings.Repeat(string(stringQuote), 3)) && !escaped {
				inString = false
				multiline = false
				stringQuote = 0
				i += 3
				continue
			}
			if !multiline && ch == stringQuote && !escaped {
				inString = false
				stringQuote = 0
				i++
				continue
			}
			if stringQuote == '"' && ch == '\\' && !escaped {
				escaped = true
				i++
				continue
			}
			escaped = false
			i++
			continue
		}

		if strings.HasPrefix(content[i:], `"""`) || strings.HasPrefix(content[i:], `'''`) {
			inString = true
			stringQuote = ch
			multiline = true
			i += 3
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			stringQuote = ch
			i++
			continue
		}
		if ch == '#' {
			return true
		}
		i++
	}
	return false
}

func formatINI(content string, st style) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errSkipFormat
	}

	cfg, err := ini.LoadSources(ini.LoadOptions{
		AllowBooleanKeys:           true,
		AllowDuplicateShadowValues: true,
		AllowNonUniqueSections:     true,
		AllowShadows:               true,
		PreserveSurroundedQuote:    true,
	}, []byte(content))
	if err != nil {
		return "", err
	}
	if iniIsEmpty(cfg) {
		return "", errSkipFormat
	}

	var buf bytes.Buffer
	if _, err := cfg.WriteToIndent(&buf, st.indent); err != nil {
		return "", err
	}
	return ensureTrailingNewline(normalizeLineEndings(buf.String())), nil
}

func iniIsEmpty(cfg *ini.File) bool {
	if cfg == nil {
		return true
	}
	for _, section := range cfg.Sections() {
		if section.Comment != "" || len(section.Keys()) > 0 {
			return false
		}
	}
	return true
}

type xmlTokenKind int

const (
	xmlTokenNone xmlTokenKind = iota
	xmlTokenStart
	xmlTokenEnd
	xmlTokenCharData
	xmlTokenOther
)

func formatXML(content string, st style) (string, error) {
	src := strings.TrimSpace(content)
	if src == "" {
		return "", errSkipFormat
	}

	formatter := newXMLFormatter(st.indent)
	dec := xml.NewDecoder(strings.NewReader(src))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if err := formatter.writeToken(tok); err != nil {
			return "", err
		}
	}
	return formatter.string()
}

type xmlFormatter struct {
	buf    bytes.Buffer
	enc    *xml.Encoder
	indent string
	depth  int
	wrote  bool
	prev   xmlTokenKind
}

func newXMLFormatter(indent string) *xmlFormatter {
	f := &xmlFormatter{indent: indent, prev: xmlTokenNone}
	f.enc = xml.NewEncoder(&f.buf)
	return f
}

func (f *xmlFormatter) writeToken(tok xml.Token) error {
	switch t := tok.(type) {
	case xml.StartElement:
		return f.writeStart(t)
	case xml.EndElement:
		return f.writeEnd(t)
	case xml.CharData:
		return f.writeCharData(t)
	case xml.Comment, xml.ProcInst, xml.Directive:
		return f.writeOther(tok, true)
	default:
		return f.writeOther(tok, false)
	}
}

func (f *xmlFormatter) writeStart(tok xml.StartElement) error {
	if f.wrote && f.prev != xmlTokenCharData {
		if err := f.writeBreak(f.depth); err != nil {
			return err
		}
	}
	if err := f.enc.EncodeToken(tok); err != nil {
		return err
	}
	f.depth++
	f.prev = xmlTokenStart
	f.wrote = true
	return nil
}

func (f *xmlFormatter) writeEnd(tok xml.EndElement) error {
	f.depth--
	if f.depth < 0 {
		return errors.New("invalid XML depth")
	}
	if f.prev != xmlTokenStart && f.prev != xmlTokenCharData {
		if err := f.writeBreak(f.depth); err != nil {
			return err
		}
	}
	if err := f.enc.EncodeToken(tok); err != nil {
		return err
	}
	f.prev = xmlTokenEnd
	f.wrote = true
	return nil
}

func (f *xmlFormatter) writeCharData(tok xml.CharData) error {
	text := string(tok)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	// Leading or trailing whitespace around text is mixed content whose spacing
	// may be significant, so skip formatting instead of changing semantics.
	if trimmed != text {
		return errSkipFormat
	}
	if err := f.enc.EncodeToken(tok); err != nil {
		return err
	}
	f.prev = xmlTokenCharData
	f.wrote = true
	return nil
}

func (f *xmlFormatter) writeOther(tok xml.Token, breakBefore bool) error {
	if breakBefore && f.wrote {
		if err := f.writeBreak(f.depth); err != nil {
			return err
		}
	}
	if err := f.enc.EncodeToken(tok); err != nil {
		return err
	}
	f.prev = xmlTokenOther
	f.wrote = true
	return nil
}

func (f *xmlFormatter) writeBreak(level int) error {
	if err := f.enc.Flush(); err != nil {
		return err
	}
	if f.wrote {
		f.buf.WriteByte('\n')
	}
	for range level {
		f.buf.WriteString(f.indent)
	}
	return nil
}

func (f *xmlFormatter) string() (string, error) {
	if err := f.enc.Flush(); err != nil {
		return "", err
	}
	return ensureTrailingNewline(f.buf.String()), nil
}

func ensureTrailingNewline(s string) string {
	s = strings.TrimRight(s, "\n")
	return s + "\n"
}

func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
