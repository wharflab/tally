// Package heredocfmt formats typed file content embedded in Dockerfile heredocs.
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
)

const defaultIndentWidth = 2

var errSkipFormat = errors.New("skip formatting")

// Kind identifies a supported heredoc payload format.
type Kind string

const (
	KindJSON Kind = "json"
	KindYAML Kind = "yaml"
	KindTOML Kind = "toml"
	KindXML  Kind = "xml"
)

// Formatter resolves EditorConfig style once per virtual target filename and formats heredoc content.
type Formatter struct {
	dockerfilePath string
	styleCache     map[string]style
}

type style struct {
	indent      string
	indentWidth int
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
	case ".xml":
		return KindXML, true
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

func (f *Formatter) styleForTarget(target string) (style, error) {
	virtualName := VirtualFilename(f.dockerfilePath, target)
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

// VirtualFilename returns the synthetic filename used for EditorConfig matching.
//
// The file lives next to the Dockerfile so .editorconfig discovery follows the
// repository layout, while the basename comes from the heredoc destination so
// selectors such as *.json, config.yaml, or package.json still match.
func VirtualFilename(dockerfilePath, target string) string {
	base := targetBasename(target)
	if base == "" {
		if kind, ok := SupportedKind(target); ok {
			base = "heredoc." + string(kind)
		} else {
			base = "heredoc"
		}
	}
	return filepath.Join(dockerfileDir(dockerfilePath), filepath.FromSlash(base))
}

func targetBasename(target string) string {
	target = strings.TrimSpace(target)
	target = strings.Trim(target, `"'`)
	target = strings.ReplaceAll(target, `\`, `/`)
	base := path.Base(target)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if _, ok := SupportedKind(base); !ok {
		return ""
	}
	return base
}

func dockerfileDir(dockerfilePath string) string {
	if dockerfilePath == "" || dockerfilePath == "-" {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	}
	if abs, err := filepath.Abs(dockerfilePath); err == nil {
		return filepath.Dir(abs)
	}
	return filepath.Dir(dockerfilePath)
}

func styleFromDefinition(def *editorconfig.Definition) style {
	if def == nil {
		return style{
			indent:      strings.Repeat(" ", defaultIndentWidth),
			indentWidth: defaultIndentWidth,
		}
	}

	width := parseIndentWidth(def)
	if strings.EqualFold(def.IndentStyle, editorconfig.IndentStyleTab) {
		return style{indent: "\t", indentWidth: width}
	}
	return style{
		indent:      strings.Repeat(" ", width),
		indentWidth: width,
	}
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
	default:
		return "", errSkipFormat
	}
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
