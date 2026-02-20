package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"

	"github.com/atombender/go-jsonschema/pkg/generator"
)

const (
	manifestPathRel = "internal/schemas/manifest.json"
	rootSchemaPath  = "internal/schemas/root/tally-config.schema.json"
	ruleConfigPath  = "internal/rules/rule-config.schema.json"
	registryOutPath = "internal/schemas/registry_gen.go"
	filePerm        = 0o644
	dirPerm         = 0o755
)

type manifest struct {
	Schemas []schemaEntry `json:"schemas"`
}

type schemaEntry struct {
	Input   string `json:"input"`
	Output  string `json:"output"`
	Package string `json:"package"`
}

type schemaIDDoc struct {
	ID string `json:"$id"`
}

type refSchema struct {
	Ref string `json:"$ref"`
}

type indexSchema struct {
	Schema               string               `json:"$schema"`
	ID                   string               `json:"$id"`
	Comment              string               `json:"$comment,omitempty"`
	Title                string               `json:"title"`
	Description          string               `json:"description,omitempty"`
	Type                 string               `json:"type"`
	Properties           map[string]refSchema `json:"properties,omitempty"`
	AdditionalProperties refSchema            `json:"additionalProperties"`
	Examples             []any                `json:"examples,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "schema-gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(repoRoot, filepath.FromSlash(manifestPathRel))
	m, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}

	if err := generateNamespaceIndexes(repoRoot, m); err != nil {
		return err
	}

	if err := generateGoTypes(repoRoot, m); err != nil {
		return err
	}

	if err := generateSchemaRegistry(repoRoot, m); err != nil {
		return err
	}

	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, filepath.FromSlash(manifestPathRel))
		if _, err := os.Stat(candidate); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find repo root containing %s", manifestPathRel)
}

func loadManifest(path string) (*manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m manifest
	if err := jsonv2.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if len(m.Schemas) == 0 {
		return nil, fmt.Errorf("manifest %s has no schemas", path)
	}
	return &m, nil
}

func generateGoTypes(repoRoot string, m *manifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}

	defaultOutputName := ""
	defaultPackageName := ""
	for _, entry := range m.Schemas {
		if filepath.ToSlash(entry.Input) != rootSchemaPath {
			continue
		}
		defaultOutputName = entry.Output
		defaultPackageName = entry.Package
		break
	}
	if defaultOutputName == "" || defaultPackageName == "" {
		return fmt.Errorf("manifest must include %s", rootSchemaPath)
	}

	ruleConfigAbs := filepath.Join(repoRoot, filepath.FromSlash(ruleConfigPath))
	ruleConfigID, err := readSchemaID(ruleConfigAbs)
	if err != nil {
		return err
	}

	mappings := make([]generator.SchemaMapping, 0, len(m.Schemas)+1)
	for _, entry := range m.Schemas {
		if entry.Input == "" || entry.Output == "" || entry.Package == "" {
			return fmt.Errorf("invalid schema manifest entry: %+v", entry)
		}

		inputAbs := filepath.Join(repoRoot, filepath.FromSlash(entry.Input))
		schemaID, err := readSchemaID(inputAbs)
		if err != nil {
			return err
		}

		mappings = append(mappings, generator.SchemaMapping{
			SchemaID:    schemaID,
			PackageName: entry.Package,
			OutputName:  entry.Output,
		})
	}

	mappings = append(mappings, generator.SchemaMapping{
		SchemaID:    ruleConfigID,
		PackageName: "github.com/wharflab/tally/internal/schemas/generated/rules/ruleschema",
		OutputName:  "internal/schemas/generated/rules/ruleschema/rule_config.gen.go",
	})

	g, err := generator.New(generator.Config{
		SchemaMappings:            mappings,
		DefaultPackageName:        defaultPackageName,
		DefaultOutputName:         defaultOutputName,
		OnlyModels:                true,
		Tags:                      []string{"json"},
		Warner:                    func(string) {},
		DisableOmitempty:          false,
		DisableCustomTypesForMaps: false,
	})
	if err != nil {
		return fmt.Errorf("create generator: %w", err)
	}

	for _, entry := range m.Schemas {
		inputAbs := filepath.Join(repoRoot, filepath.FromSlash(entry.Input))
		if err := g.DoFile(inputAbs); err != nil {
			return fmt.Errorf("generate %s: %w", entry.Input, err)
		}
	}

	sources, err := g.Sources()
	if err != nil {
		return fmt.Errorf("render sources: %w", err)
	}
	if len(sources) == 0 {
		return fmt.Errorf("generator produced no sources")
	}

	for outputName, source := range sources {
		if bytes.Contains(source, []byte(`"encoding/json"`)) {
			return fmt.Errorf("generated file %s imports encoding/json", outputName)
		}

		outputAbs := filepath.Join(repoRoot, filepath.FromSlash(outputName))
		if err := os.MkdirAll(filepath.Dir(outputAbs), dirPerm); err != nil {
			return fmt.Errorf("create output dir for %s: %w", outputName, err)
		}
		if err := os.WriteFile(outputAbs, source, filePerm); err != nil {
			return fmt.Errorf("write generated file %s: %w", outputName, err)
		}
	}

	return nil
}

func generateNamespaceIndexes(repoRoot string, m *manifest) error {
	namespaces := []string{"tally", "hadolint", "buildkit"}

	filesByNamespace := make(map[string][]string, len(namespaces))
	for _, entry := range m.Schemas {
		ns, filename, ok := parseRuleSchemaInput(entry.Input)
		if !ok {
			continue
		}
		filesByNamespace[ns] = append(filesByNamespace[ns], filename)
	}
	for ns := range filesByNamespace {
		sort.Strings(filesByNamespace[ns])
	}

	for _, ns := range namespaces {
		outputRel := filepath.ToSlash(filepath.Join("internal/rules", ns, "index.schema.json"))
		outputAbs := filepath.Join(repoRoot, filepath.FromSlash(outputRel))

		props := make(map[string]refSchema)
		for _, filename := range filesByNamespace[ns] {
			name := ruleNameFromSchemaFilename(ns, filename)
			props[name] = refSchema{Ref: "./" + filename}
		}

		idx := indexSchema{
			Schema:  "https://json-schema.org/draft/2020-12/schema",
			ID:      "https://schemas.tally.dev/rules/" + ns + "/index.schema.json",
			Comment: "Code generated by _tools/schema-gen. DO NOT EDIT.",
			Title:   ns + "/* rule namespace config",
			Description: "Schema for rules." + ns + " configuration; keys are rule names within the " +
				ns + " namespace.",
			Type:                 "object",
			Properties:           props,
			AdditionalProperties: refSchema{Ref: "../rule-config.schema.json#/$defs/genericRuleConfig"},
			Examples: []any{
				map[string]any{
					ruleExampleKey(ns): map[string]any{
						"severity": "warning",
					},
				},
			},
		}

		if err := writeFormattedJSONFile(outputAbs, &idx); err != nil {
			return fmt.Errorf("write %s: %w", outputRel, err)
		}
	}

	return nil
}

func generateSchemaRegistry(repoRoot string, m *manifest) error {
	rootAbs := filepath.Join(repoRoot, filepath.FromSlash(rootSchemaPath))
	rootID, err := readSchemaID(rootAbs)
	if err != nil {
		return err
	}

	paths := []string{
		rootSchemaPath,
		ruleConfigPath,
		filepath.ToSlash(filepath.Join("internal/rules/tally/index.schema.json")),
		filepath.ToSlash(filepath.Join("internal/rules/hadolint/index.schema.json")),
		filepath.ToSlash(filepath.Join("internal/rules/buildkit/index.schema.json")),
	}

	for _, entry := range m.Schemas {
		if _, _, ok := parseRuleSchemaInput(entry.Input); ok {
			paths = append(paths, entry.Input)
		}
	}

	uniquePaths := make(map[string]struct{}, len(paths))
	deduped := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.ToSlash(p)
		if _, ok := uniquePaths[p]; ok {
			continue
		}
		uniquePaths[p] = struct{}{}
		deduped = append(deduped, p)
	}
	paths = deduped

	schemaBytesByID := make(map[string][]byte, len(paths))
	for _, rel := range paths {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read schema %s: %w", rel, err)
		}
		id, err := readSchemaIDFromBytes(rel, data)
		if err != nil {
			return err
		}
		if _, exists := schemaBytesByID[id]; exists {
			return fmt.Errorf("duplicate schema $id %q (from %s)", id, rel)
		}
		schemaBytesByID[id] = data
	}

	ruleSchemaIDs := make(map[string]string)
	for _, entry := range m.Schemas {
		ns, filename, ok := parseRuleSchemaInput(entry.Input)
		if !ok {
			continue
		}
		abs := filepath.Join(repoRoot, filepath.FromSlash(entry.Input))
		id, err := readSchemaID(abs)
		if err != nil {
			return err
		}
		ruleCode := ns + "/" + ruleNameFromSchemaFilename(ns, filename)
		ruleSchemaIDs[ruleCode] = id
	}

	outAbs := filepath.Join(repoRoot, filepath.FromSlash(registryOutPath))
	if err := os.MkdirAll(filepath.Dir(outAbs), dirPerm); err != nil {
		return fmt.Errorf("create output dir for %s: %w", outAbs, err)
	}

	source, err := renderRegistryGo(rootID, ruleSchemaIDs, schemaBytesByID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outAbs, source, filePerm); err != nil {
		return fmt.Errorf("write registry %s: %w", registryOutPath, err)
	}

	return nil
}

func renderRegistryGo(rootSchemaID string, ruleSchemaIDs map[string]string, schemaBytesByID map[string][]byte) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Code generated by _tools/schema-gen. DO NOT EDIT.\n\n")
	b.WriteString("package schemas\n\n")
	b.WriteString("const RootConfigSchemaID = " + fmt.Sprintf("%q", rootSchemaID) + "\n\n")

	b.WriteString("var ruleSchemaIDs = map[string]string{\n")
	ruleCodes := make([]string, 0, len(ruleSchemaIDs))
	for code := range ruleSchemaIDs {
		ruleCodes = append(ruleCodes, code)
	}
	sort.Strings(ruleCodes)
	for _, code := range ruleCodes {
		b.WriteString("\t" + fmt.Sprintf("%q: %q,", code, ruleSchemaIDs[code]) + "\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("var schemaBytesByID = map[string][]byte{\n")
	schemaIDs := make([]string, 0, len(schemaBytesByID))
	for id := range schemaBytesByID {
		schemaIDs = append(schemaIDs, id)
	}
	sort.Strings(schemaIDs)
	for _, id := range schemaIDs {
		b.WriteString("\t" + fmt.Sprintf("%q: []byte(`", id) + "\n")
		b.Write(schemaBytesByID[id])
		if len(schemaBytesByID[id]) == 0 || schemaBytesByID[id][len(schemaBytesByID[id])-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteString("`),\n")
	}
	b.WriteString("}\n")

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format registry: %w", err)
	}
	return formatted, nil
}

func parseRuleSchemaInput(inputRel string) (namespace string, filename string, ok bool) {
	path := filepath.ToSlash(inputRel)
	const prefix = "internal/rules/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	namespace = parts[0]
	filename = parts[1]
	if !strings.HasSuffix(filename, ".schema.json") {
		return "", "", false
	}
	if filename == "index.schema.json" || filename == "rule-config.schema.json" {
		return "", "", false
	}
	return namespace, filename, true
}

func ruleNameFromSchemaFilename(namespace, filename string) string {
	base := strings.TrimSuffix(filename, ".schema.json")
	if namespace == "hadolint" {
		return strings.ToUpper(base)
	}
	return base
}

func ruleExampleKey(namespace string) string {
	switch namespace {
	case "tally":
		return "max-lines"
	case "hadolint":
		return "DL3026"
	case "buildkit":
		return "StageNameCasing"
	default:
		return "example-rule"
	}
}

func readSchemaID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read schema %s: %w", path, err)
	}
	return readSchemaIDFromBytes(path, data)
}

func readSchemaIDFromBytes(path string, data []byte) (string, error) {
	var doc schemaIDDoc
	if err := jsonv2.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse schema %s: %w", path, err)
	}
	if doc.ID == "" {
		return "", fmt.Errorf("schema %s missing $id", path)
	}
	return doc.ID, nil
}

func writeFormattedJSONFile(path string, v any) error {
	data, err := jsonv2.Marshal(v)
	if err != nil {
		return err
	}
	formatted, err := jsontext.AppendFormat(nil, data, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	formatted = append(formatted, '\n')

	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	if err := os.WriteFile(path, formatted, filePerm); err != nil {
		return err
	}
	return nil
}
