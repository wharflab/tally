package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	jsonv2 "encoding/json/v2"

	"github.com/atombender/go-jsonschema/pkg/generator"
)

const (
	manifestPath = "internal/schemas/manifest.json"
	filePerm     = 0o644
	dirPerm      = 0o755
)

type manifest struct {
	Schemas []schemaEntry `json:"schemas"`
}

type schemaEntry struct {
	Input   string `json:"input"`
	Output  string `json:"output"`
	Package string `json:"package"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "schema-gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	m, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}

	for _, entry := range m.Schemas {
		if err := generateOne(entry); err != nil {
			return err
		}
	}

	return nil
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

func generateOne(entry schemaEntry) error {
	if entry.Input == "" || entry.Output == "" || entry.Package == "" {
		return fmt.Errorf("invalid schema manifest entry: %+v", entry)
	}

	g, err := generator.New(generator.Config{
		DefaultPackageName: entry.Package,
		DefaultOutputName:  entry.Output,
		OnlyModels:         true,
		Tags:               []string{"json"},
	})
	if err != nil {
		return fmt.Errorf("create generator for %s: %w", entry.Input, err)
	}

	if err := g.DoFile(entry.Input); err != nil {
		return fmt.Errorf("generate %s: %w", entry.Input, err)
	}

	sources, err := g.Sources()
	if err != nil {
		return fmt.Errorf("render sources for %s: %w", entry.Input, err)
	}

	source, ok := sources[entry.Output]
	if !ok {
		return fmt.Errorf("generator did not return expected output %s", entry.Output)
	}

	if bytes.Contains(source, []byte(`"encoding/json"`)) {
		return fmt.Errorf("generated file %s imports encoding/json", entry.Output)
	}

	if err := os.MkdirAll(filepath.Dir(entry.Output), dirPerm); err != nil {
		return fmt.Errorf("create output dir for %s: %w", entry.Output, err)
	}
	if err := os.WriteFile(entry.Output, source, filePerm); err != nil {
		return fmt.Errorf("write generated file %s: %w", entry.Output, err)
	}

	return nil
}
