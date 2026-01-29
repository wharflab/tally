package dockerfile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/config"
)

func TestParse_BasicParsing(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "simple dockerfile",
			content: "FROM alpine:3.18\nRUN echo hello\n",
		},
		{
			name:    "multiline dockerfile",
			content: "FROM alpine:3.18\nRUN apk add --no-cache \\\n    curl \\\n    wget\nCMD [\"sh\"]\n",
		},
		{
			name:    "single line no newline",
			content: "FROM alpine:3.18",
		},
		{
			name:    "empty lines",
			content: "FROM alpine:3.18\n\n\nRUN echo hello\n",
		},
		{
			name:    "with comments",
			content: "# This is a comment\nFROM alpine:3.18\n# Another comment\nRUN echo hello\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			result, err := ParseFile(context.Background(), dockerfilePath, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			// Basic validation: AST should be populated
			if result.AST == nil {
				t.Error("AST is nil")
			}
			if result.AST.AST == nil {
				t.Error("AST.AST is nil")
			}
			if result.Source == nil {
				t.Error("Source is nil")
			}
		})
	}
}

func TestParse_Stages(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectedStages int
		stageNames     []string
	}{
		{
			name:           "single stage",
			content:        "FROM alpine:3.18\nRUN echo hello\n",
			expectedStages: 1,
			stageNames:     []string{""},
		},
		{
			name:           "named single stage",
			content:        "FROM alpine:3.18 AS builder\nRUN echo hello\n",
			expectedStages: 1,
			stageNames:     []string{"builder"},
		},
		{
			name:           "multi-stage build",
			content:        "FROM golang:1.21 AS builder\nRUN go build\n\nFROM alpine:3.18\nCOPY --from=builder /app /app\n",
			expectedStages: 2,
			stageNames:     []string{"builder", ""},
		},
		{
			name: "three named stages",
			content: "FROM node:20 AS deps\nRUN npm ci\n\n" +
				"FROM node:20 AS builder\nCOPY --from=deps /app/node_modules ./node_modules\n" +
				"RUN npm run build\n\nFROM node:20-slim AS runtime\nCOPY --from=builder /app/dist ./dist\n",
			expectedStages: 3,
			stageNames:     []string{"deps", "builder", "runtime"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			result, err := ParseFile(context.Background(), dockerfilePath, nil)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}

			if len(result.Stages) != tt.expectedStages {
				t.Errorf("len(Stages) = %d, want %d", len(result.Stages), tt.expectedStages)
			}

			for i, name := range tt.stageNames {
				if i < len(result.Stages) && result.Stages[i].Name != name {
					t.Errorf("Stages[%d].Name = %q, want %q", i, result.Stages[i].Name, name)
				}
			}
		})
	}
}

func TestParse_MetaArgs(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		expectedArgs []string
	}{
		{
			name:         "no meta args",
			content:      "FROM alpine:3.18\nRUN echo hello\n",
			expectedArgs: nil,
		},
		{
			name:         "single meta arg",
			content:      "ARG VERSION=1.0\nFROM alpine:${VERSION}\n",
			expectedArgs: []string{"VERSION"},
		},
		{
			name:         "multiple meta args",
			content:      "ARG BASE_IMAGE=alpine\nARG VERSION=3.18\nFROM ${BASE_IMAGE}:${VERSION}\n",
			expectedArgs: []string{"BASE_IMAGE", "VERSION"},
		},
		{
			name:         "args after FROM are not meta args",
			content:      "ARG VERSION=1.0\nFROM alpine:${VERSION}\nARG BUILD_TYPE=release\nRUN echo $BUILD_TYPE\n",
			expectedArgs: []string{"VERSION"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			result, err := ParseFile(context.Background(), dockerfilePath, nil)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}

			if len(result.MetaArgs) != len(tt.expectedArgs) {
				t.Errorf("len(MetaArgs) = %d, want %d", len(result.MetaArgs), len(tt.expectedArgs))
			}

			for i, name := range tt.expectedArgs {
				if i < len(result.MetaArgs) && len(result.MetaArgs[i].Args) > 0 {
					if result.MetaArgs[i].Args[0].Key != name {
						t.Errorf("MetaArgs[%d].Args[0].Key = %q, want %q", i, result.MetaArgs[i].Args[0].Key, name)
					}
				}
			}
		})
	}
}

func TestParse_BuildKitWarnings(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		expectedWarnings int
		wantRuleName     string
	}{
		{
			name:             "no warnings",
			content:          "FROM alpine:3.18\nRUN echo hello\n",
			expectedWarnings: 0,
		},
		{
			name:             "MAINTAINER deprecated",
			content:          "FROM alpine:3.18\nMAINTAINER test@example.com\n",
			expectedWarnings: 1,
			wantRuleName:     "MaintainerDeprecated",
		},
		{
			name:             "stage name casing",
			content:          "FROM alpine:3.18 AS Builder\nRUN echo hello\n",
			expectedWarnings: 1,
			wantRuleName:     "StageNameCasing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			result, err := ParseFile(context.Background(), dockerfilePath, nil)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}

			if len(result.Warnings) != tt.expectedWarnings {
				t.Errorf("len(Warnings) = %d, want %d", len(result.Warnings), tt.expectedWarnings)
				for i, w := range result.Warnings {
					t.Logf("  Warning[%d]: %s - %s", i, w.RuleName, w.Message)
				}
			}

			if tt.wantRuleName != "" && len(result.Warnings) > 0 {
				if result.Warnings[0].RuleName != tt.wantRuleName {
					t.Errorf("Warnings[0].RuleName = %q, want %q", result.Warnings[0].RuleName, tt.wantRuleName)
				}
			}
		})
	}
}

func TestParse_Source(t *testing.T) {
	content := "FROM alpine:3.18\nRUN echo hello\n"
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ParseFile(context.Background(), dockerfilePath, nil)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if string(result.Source) != content {
		t.Errorf("Source = %q, want %q", string(result.Source), content)
	}
}

func TestParse_SkipsDisabledBuildKitRules(t *testing.T) {
	// Dockerfile that triggers StageNameCasing warning (uppercase stage name)
	content := "FROM alpine:3.18 AS MyBuild\nRUN echo hello\n"

	// Parse without config - should get the warning
	resultNoConfig, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	hasWarning := false
	for _, w := range resultNoConfig.Warnings {
		if w.RuleName == "StageNameCasing" {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected StageNameCasing warning without config, got none")
	}

	// Parse with StageNameCasing disabled - should NOT get the warning
	cfg := config.Default()
	cfg.Rules.Exclude = append(cfg.Rules.Exclude, "buildkit/StageNameCasing")

	resultWithConfig, err := Parse(strings.NewReader(content), cfg)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, w := range resultWithConfig.Warnings {
		if w.RuleName == "StageNameCasing" {
			t.Error("expected StageNameCasing warning to be skipped when disabled, but it was reported")
		}
	}
}

func TestParse_EnablesExperimentalRules(t *testing.T) {
	// Dockerfile that triggers InvalidDefinitionDescription (experimental rule)
	// This rule checks that comments for ARG/stage follow format: # name description
	content := `# wrong format comment
ARG MY_ARG
FROM alpine:3.18
`

	// Parse without config - experimental rule should NOT trigger
	resultNoConfig, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	hasWarning := false
	for _, w := range resultNoConfig.Warnings {
		if w.RuleName == "InvalidDefinitionDescription" {
			hasWarning = true
			break
		}
	}
	if hasWarning {
		t.Error("expected no InvalidDefinitionDescription warning without enabling experimental, but got one")
	}

	// Parse with experimental rule enabled via include
	cfg := config.Default()
	cfg.Rules.Include = append(cfg.Rules.Include, "buildkit/InvalidDefinitionDescription")

	resultWithConfig, err := Parse(strings.NewReader(content), cfg)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	hasWarning = false
	for _, w := range resultWithConfig.Warnings {
		if w.RuleName == "InvalidDefinitionDescription" {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected InvalidDefinitionDescription warning when experimental enabled, got none")
	}
}
