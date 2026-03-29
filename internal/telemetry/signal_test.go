package telemetry

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

type mockBuildContext struct {
	files map[string]string
}

func (m *mockBuildContext) IsIgnored(string) (bool, error) { return false, nil }

func (m *mockBuildContext) FileExists(path string) bool {
	_, ok := m.files[path]
	return ok
}

func (m *mockBuildContext) ReadFile(path string) ([]byte, error) {
	content, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("missing file %q", path)
	}
	return []byte(content), nil
}

func (m *mockBuildContext) IsHeredocFile(string) bool { return false }

func (m *mockBuildContext) HasIgnoreFile() bool { return false }

func (m *mockBuildContext) HasIgnoreExclusions() bool { return false }

func TestDetectStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		content        string
		contextFiles   map[string]string
		wantTools      []ToolID
		wantAnchorLine int
	}{
		{
			name: "bun direct command",
			content: `FROM oven/bun:1
RUN bun install
`,
			wantTools:      []ToolID{ToolBun},
			wantAnchorLine: 2,
		},
		{
			name: "azure cli package install",
			content: `FROM ubuntu:24.04
RUN apt-get update && apt-get install -y azure-cli
`,
			wantTools:      []ToolID{ToolAzureCLI},
			wantAnchorLine: 2,
		},
		{
			name: "wrangler via npx",
			content: `FROM node:22
RUN npx wrangler deploy
`,
			wantTools:      []ToolID{ToolWrangler},
			wantAnchorLine: 2,
		},
		{
			name: "hugging face via python module",
			content: `FROM python:3.12
RUN python -m huggingface_hub scan-cache
`,
			wantTools:      []ToolID{ToolHuggingFace},
			wantAnchorLine: 2,
		},
		{
			name: "next from package json plus npm run build",
			content: `FROM node:22
WORKDIR /app
COPY package.json ./package.json
RUN npm run build
`,
			contextFiles: map[string]string{
				"package.json": `{"dependencies":{"next":"15.0.0"}}`,
			},
			wantTools:      []ToolID{ToolNextJS},
			wantAnchorLine: 3,
		},
		{
			name: "yarn berry from package manager metadata",
			content: `FROM node:22
WORKDIR /app
COPY package.json ./package.json
RUN yarn install --immutable
`,
			contextFiles: map[string]string{
				"package.json": `{"packageManager":"yarn@4.2.2"}`,
			},
			wantTools:      []ToolID{ToolYarnBerry},
			wantAnchorLine: 3,
		},
		{
			name: "bare yarn without berry evidence stays quiet",
			content: `FROM node:22
RUN yarn install
`,
			wantTools: nil,
		},
		{
			name: "generic python requirements stay quiet",
			content: `FROM python:3.12
WORKDIR /app
COPY requirements.txt ./requirements.txt
RUN pip install -r requirements.txt
`,
			contextFiles: map[string]string{
				"requirements.txt": "flask==3.0.0\n",
			},
			wantTools: nil,
		},
		{
			name: "similar package names and comments stay quiet",
			content: `FROM python:3.12
WORKDIR /app
COPY requirements.txt ./requirements.txt
RUN pip install -r requirements.txt
`,
			contextFiles: map[string]string{
				"requirements.txt": "# transformers intentionally excluded\ntensorflow-datasets==4.9.0\n",
			},
			wantTools: nil,
		},
		{
			name: "node only huggingface hub stays quiet",
			content: `FROM node:22
RUN npx @huggingface/hub upload
`,
			wantTools: nil,
		},
		{
			name: "windows vcpkg bootstrap command",
			content: "# escape=`\n" + `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN bootstrap-vcpkg.bat
`,
			wantTools:      []ToolID{ToolVcpkg},
			wantAnchorLine: 3,
		},
		{
			name: "container start via npx wrangler",
			content: `FROM node:22
CMD ["npx", "wrangler", "deploy"]
`,
			wantTools:      []ToolID{ToolWrangler},
			wantAnchorLine: 2,
		},
		{
			name: "container start via python module",
			content: `FROM python:3.12
ENTRYPOINT ["python", "-m", "huggingface_hub", "scan-cache"]
`,
			wantTools:      []ToolID{ToolHuggingFace},
			wantAnchorLine: 2,
		},
		{
			name: "shell instruction powershell",
			content: `FROM ubuntu:24.04
SHELL ["pwsh", "-Command"]
`,
			wantTools:      []ToolID{ToolPowerShell},
			wantAnchorLine: 2,
		},
		{
			name: "container start via homebrew",
			content: `FROM ubuntu:24.04
CMD ["brew", "install", "jq"]
`,
			wantTools:      []ToolID{ToolHomebrew},
			wantAnchorLine: 2,
		},
		{
			name: "next from package json plus cmd npm start",
			content: `FROM node:22
WORKDIR /app
COPY package.json ./package.json
CMD ["npm", "start"]
`,
			contextFiles: map[string]string{
				"package.json": `{"dependencies":{"next":"15.0.0"}}`,
			},
			wantTools:      []ToolID{ToolNextJS},
			wantAnchorLine: 3,
		},
		{
			name: "next from package json plus cmd npm workspace start",
			content: `FROM node:22
WORKDIR /app
COPY package.json ./package.json
CMD ["npm", "--workspace", "web", "start"]
`,
			contextFiles: map[string]string{
				"package.json": `{"dependencies":{"next":"15.0.0"}}`,
			},
			wantTools:      []ToolID{ToolNextJS},
			wantAnchorLine: 3,
		},
		{
			name: "yarn berry from package manager metadata plus entrypoint yarn start",
			content: `FROM node:22
WORKDIR /app
COPY package.json ./package.json
ENTRYPOINT ["yarn", "start"]
`,
			contextFiles: map[string]string{
				"package.json": `{"packageManager":"yarn@4.2.2"}`,
			},
			wantTools:      []ToolID{ToolYarnBerry},
			wantAnchorLine: 3,
		},
		{
			name: "hugging face requirements manifest plus pip install",
			content: `FROM python:3.12
WORKDIR /app
COPY requirements.txt ./requirements.txt
RUN pip install -r requirements.txt
`,
			contextFiles: map[string]string{
				"requirements.txt": "transformers==4.51.0\n",
			},
			wantTools:      []ToolID{ToolHuggingFace},
			wantAnchorLine: 3,
		},
		{
			name: "yarn berry from copied release plus corepack enable",
			content: `FROM node:22
WORKDIR /app
COPY .yarn/releases/yarn-4.2.2.cjs ./.yarn/releases/yarn-4.2.2.cjs
RUN corepack enable
`,
			contextFiles: map[string]string{
				".yarn/releases/yarn-4.2.2.cjs": "",
			},
			wantTools:      []ToolID{ToolYarnBerry},
			wantAnchorLine: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var ctx rules.BuildContext
			if len(tt.contextFiles) > 0 {
				ctx = &mockBuildContext{files: tt.contextFiles}
			}

			input := testutil.MakeLintInputWithContext(t, "Dockerfile", tt.content, ctx)
			stageFacts := input.Facts.Stage(0)
			signals := DetectStage(input.Stages[0], stageFacts, input.Semantic.StageInfo(0))

			if got := signals.OrderedToolIDs(); !slices.Equal(got, tt.wantTools) {
				t.Fatalf("tools = %v, want %v", got, tt.wantTools)
			}

			anchor, ok := signals.Anchor()
			if len(tt.wantTools) == 0 {
				if ok {
					t.Fatalf("unexpected anchor = %+v", anchor)
				}
				return
			}
			if !ok {
				t.Fatal("expected anchor signal")
			}
			if anchor.Line != tt.wantAnchorLine {
				t.Fatalf("anchor line = %d, want %d", anchor.Line, tt.wantAnchorLine)
			}
		})
	}
}

func TestDetectStageWithoutFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantTools []ToolID
	}{
		{
			name: "direct bun run still detected",
			content: `FROM oven/bun:1
RUN bun install
`,
			wantTools: []ToolID{ToolBun},
		},
		{
			name: "npx wrangler run still detected",
			content: `FROM node:22
RUN npx wrangler deploy
`,
			wantTools: []ToolID{ToolWrangler},
		},
		{
			name: "dotnet run still detected",
			content: `FROM mcr.microsoft.com/dotnet/sdk:8.0
RUN dotnet restore
`,
			wantTools: []ToolID{ToolDotNetCLI},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			signals := DetectStage(input.Stages[0], nil, input.Semantic.StageInfo(0))

			if got := signals.OrderedToolIDs(); !slices.Equal(got, tt.wantTools) {
				t.Fatalf("tools = %v, want %v", got, tt.wantTools)
			}
		})
	}
}
