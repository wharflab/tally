package tally

import (
	"context"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferPackageCacheMountsRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferPackageCacheMountsRule().Metadata())
}

func TestPreferPackageCacheMountsRule_Check(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewPreferPackageCacheMountsRule(), []testutil.RuleTestCase{
		{
			Name: "npm install without cache mount",
			Content: `FROM node:20
RUN npm install
`,
			WantViolations: 1,
		},
		{
			Name: "npm install with cache mount already present",
			Content: `FROM node:20
RUN --mount=type=cache,target=/root/.npm npm ci
`,
			WantViolations: 0,
		},
		{
			Name: "npm i alias without cache mount",
			Content: `FROM node:20
RUN npm i
`,
			WantViolations: 1,
		},
		{
			Name: "go build without cache mounts",
			Content: `FROM golang:1.24
RUN go build ./...
`,
			WantViolations: 1,
		},
		{
			Name: "apt install with partial mounts",
			Content: `FROM ubuntu:24.04
RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf \
    --mount=type=cache,target=/var/cache/apt \
    apt-get update && apt-get install -y gcc
`,
			WantViolations: 1,
		},
		{
			Name: "apt install with both locked mounts",
			Content: `FROM ubuntu:24.04
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y gcc
`,
			WantViolations: 0,
		},
		{
			Name: "apt install with escaped newline and leading &&",
			Content: `FROM debian:13-slim

RUN apt-get update \
	&& apt-get install -y binaryen build-essential curl git jq unzip xz-utils zstd

WORKDIR /ghc
`,
			WantViolations: 1,
		},
		{
			Name: "apk add",
			Content: `FROM alpine:3.21
RUN apk add --no-cache curl
`,
			WantViolations: 1,
		},
		{
			Name: "dnf install",
			Content: `FROM fedora:41
RUN dnf install -y gcc
`,
			WantViolations: 1,
		},
		{
			Name: "yum install",
			Content: `FROM centos:7
RUN yum install -y gcc
`,
			WantViolations: 1,
		},
		{
			Name: "zypper install",
			Content: `FROM opensuse/leap:15.6
RUN zypper install -y git
`,
			WantViolations: 1,
		},
		{
			Name: "pip install",
			Content: `FROM python:3.13
RUN pip install -r requirements.txt
`,
			WantViolations: 1,
		},
		{
			Name: "bundle install",
			Content: `FROM ruby:3.4
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "yarn install",
			Content: `FROM node:20
RUN yarn install
`,
			WantViolations: 1,
		},
		{
			Name: "cargo build",
			Content: `FROM rust:1.83
WORKDIR /app
RUN cargo build --release
`,
			WantViolations: 1,
		},
		{
			Name: "cargo build with non-default workdir",
			Content: `FROM rust:1.83
WORKDIR /src
RUN cargo build --release
`,
			WantViolations: 1,
		},
		{
			Name: "cargo build with unresolved workdir variable",
			Content: `FROM rust:1.83
ARG APP_DIR=/workspace
WORKDIR ${APP_DIR}
RUN cargo build --release
`,
			WantViolations: 1,
		},
		{
			Name: "cargo test with build filter should not trigger",
			Content: `FROM rust:1.83
WORKDIR /app
RUN cargo test build
`,
			WantViolations: 0,
		},
		{
			Name: "dotnet restore",
			Content: `FROM mcr.microsoft.com/dotnet/sdk:9.0
RUN dotnet restore
`,
			WantViolations: 1,
		},
		{
			Name: "composer install",
			Content: `FROM php:8.4
RUN composer install --no-dev
`,
			WantViolations: 1,
		},
		{
			Name: "uv pip install",
			Content: `FROM python:3.13
RUN uv pip install -r requirements.txt
`,
			WantViolations: 1,
		},
		{
			Name: "uv sync",
			Content: `FROM python:3.13
RUN uv sync --frozen
`,
			WantViolations: 1,
		},
		{
			Name: "uv python install",
			Content: `FROM python:3.13
RUN uv python install 3.12
`,
			WantViolations: 1,
		},
		{
			Name: "bun install",
			Content: `FROM oven/bun:1.2
RUN bun install
`,
			WantViolations: 1,
		},
		{
			Name: "pnpm install",
			Content: `FROM node:20
RUN pnpm install
`,
			WantViolations: 1,
		},
		{
			Name: "pnpm install with PNPM_HOME",
			Content: `FROM node:20
ENV PNPM_HOME="/pnpm"
RUN pnpm install --frozen-lockfile
`,
			WantViolations: 1,
		},
		{
			Name: "pnpm install with cache mount already present",
			Content: `FROM node:20
RUN --mount=type=cache,target=/root/.pnpm-store pnpm install
`,
			WantViolations: 0,
		},
		{
			Name: "pnpm i alias without cache mount",
			Content: `FROM node:20
RUN pnpm i
`,
			WantViolations: 1,
		},
		{
			Name: "pnpm add",
			Content: `FROM node:20
RUN pnpm add express
`,
			WantViolations: 1,
		},
		{
			Name: "non-package command",
			Content: `FROM alpine
RUN echo hello
`,
			WantViolations: 0,
		},
		{
			Name: "exec form run ignored",
			Content: `FROM node:20
RUN ["npm", "install"]
`,
			WantViolations: 0,
		},
		{
			Name: "windows powershell stage ignored",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command"]
RUN npm install
`,
			WantViolations: 0,
		},
		{
			Name: "heredoc run",
			Content: `FROM python:3.13
RUN <<EOF
pip install -r requirements.txt
EOF
`,
			WantViolations: 1,
		},
		{
			Name: "pip install with PIP_NO_CACHE_DIR env",
			Content: `FROM python:3.13
ENV PIP_NO_CACHE_DIR=1
RUN pip install -r requirements.txt
`,
			WantViolations: 1,
		},
		{
			Name: "npm install with npm_config_cache env",
			Content: `FROM node:20
ENV npm_config_cache=/tmp/npm-cache
RUN npm install
`,
			WantViolations: 1,
		},
		{
			Name: "bun install with BUN_INSTALL_CACHE_DIR env",
			Content: `FROM oven/bun:1.2
ENV BUN_INSTALL_CACHE_DIR=/tmp/bun-cache
RUN bun install
`,
			WantViolations: 1,
		},
	})
}

// resolveIfAsync materializes edits for assertions: returns fix.Edits for
// sync fixes, and invokes the registered resolver against the original
// content for async (NeedsResolve) fixes.
func resolveIfAsync(t *testing.T, fix *rules.SuggestedFix, content string) []rules.TextEdit {
	t.Helper()
	if !fix.NeedsResolve {
		return fix.Edits
	}
	resolver := fixpkg.GetResolver(fix.ResolverID)
	if resolver == nil {
		t.Fatalf("resolver %q not registered", fix.ResolverID)
	}
	resolved, err := resolver.Resolve(context.Background(), fixpkg.ResolveContext{
		FilePath: "Dockerfile",
		Content:  []byte(content),
	}, fix)
	if err != nil {
		t.Fatalf("resolver error: %v", err)
	}
	return resolved
}

func applyResolvedFix(t *testing.T, fix *rules.SuggestedFix, content string) string {
	t.Helper()
	edits := resolveIfAsync(t, fix, content)
	return string(fixpkg.ApplyEdits([]byte(content), edits))
}

func TestPreferPackageCacheMountsRule_CheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewPreferPackageCacheMountsRule()

	tests := []struct {
		name            string
		content         string
		wantFixContains []string
		wantNotContains []string
		wantEditCount   int // 0 means default (1)
	}{
		{
			name: "npm install adds mount and removes cache clean",
			content: `FROM node:20
RUN npm ci && npm cache clean --force
`,
			wantFixContains: []string{"--mount=type=cache,target=/root/.npm,id=npm"},
			wantNotContains: []string{"npm cache clean"},
			wantEditCount:   0,
		},
		{
			name: "apt extends mounts and keeps secret mount",
			content: `FROM ubuntu:24.04
RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf \
    --mount=type=cache,target=/var/cache/apt \
    apt-get update && apt-get install -y gcc && apt-get clean
`,
			wantFixContains: []string{
				"--mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf",
				"--mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked",
				"--mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked",
			},
			wantNotContains: []string{},
			wantEditCount:   0,
		},
		{
			name: "multiline chain preserves continuation style",
			content: `FROM ubuntu:24.04
RUN apt-get update && \
    apt-get install -y gcc && \
    apt-get clean
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked",
				"--mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked",
			},
			wantNotContains: []string{"apt-get clean", "apt-get update && apt-get install -y gcc"},
			wantEditCount:   0,
		},
		{
			name: "apk cleanup and no-cache flag removed",
			content: `FROM alpine:3.21
RUN apk add --no-cache curl && rm -rf /var/cache/apk/*
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/var/cache/apk,id=apk,sharing=locked",
				"apk add curl",
			},
			wantNotContains: []string{"--no-cache", "/var/cache/apk/*"},
			wantEditCount:   0,
		},
		{
			name: "dnf cleanup removed",
			content: `FROM fedora:41
RUN dnf install -y git && dnf clean all
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/var/cache/dnf,id=dnf,sharing=locked",
			},
			wantNotContains: []string{"dnf clean"},
			wantEditCount:   0,
		},
		{
			name: "yum cleanup removed",
			content: `FROM centos:7
RUN yum install -y make && yum clean all
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/var/cache/yum,id=yum,sharing=locked",
			},
			wantNotContains: []string{"yum clean"},
			wantEditCount:   0,
		},
		{
			name: "zypper cleanup removed",
			content: `FROM opensuse/leap:15.6
RUN zypper install -y git && zypper clean --all
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/var/cache/zypp,id=zypper,sharing=locked",
			},
			wantNotContains: []string{"zypper clean"},
			wantEditCount:   0,
		},
		{
			name: "cargo target follows workdir",
			content: `FROM rust:1.83
WORKDIR /workspace
RUN cargo build --release
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/workspace/target,id=cargo-target",
				"--mount=type=cache,target=/usr/local/cargo/git/db,id=cargo-git",
				"--mount=type=cache,target=/usr/local/cargo/registry,id=cargo-registry",
			},
			wantNotContains: []string{"--mount=type=cache,target=/app/target"},
		},
		{
			name: "cargo unresolved workdir skips target mount",
			content: `FROM rust:1.83
ARG APP_DIR=/workspace
WORKDIR ${APP_DIR}
RUN cargo build --release
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/usr/local/cargo/git/db,id=cargo-git",
				"--mount=type=cache,target=/usr/local/cargo/registry,id=cargo-registry",
			},
			wantNotContains: []string{
				"--mount=type=cache,target=/${APP_DIR}/target",
				"--mount=type=cache,target=/app/target",
			},
		},
		{
			name: "pip no-cache-dir and cleanup removed",
			content: `FROM python:3.13
RUN pip install --no-cache-dir -r requirements.txt && pip cache purge
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/pip,id=pip",
				"pip install -r requirements.txt",
			},
			wantNotContains: []string{"--no-cache-dir", "pip cache purge"},
			wantEditCount:   0,
		},
		{
			name: "yarn cleanup removed",
			content: `FROM node:20
RUN yarn install && yarn cache clean
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/usr/local/share/.cache/yarn,id=yarn",
			},
			wantNotContains: []string{"yarn cache clean"},
			wantEditCount:   0,
		},
		{
			name: "pnpm cleanup removed with default store",
			content: `FROM node:20
RUN pnpm install --frozen-lockfile && pnpm store prune
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.pnpm-store,id=pnpm",
			},
			wantNotContains: []string{"pnpm store prune"},
			wantEditCount:   0,
		},
		{
			name: "pnpm with PNPM_HOME resolves store path",
			content: `FROM node:20
ENV PNPM_HOME="/pnpm"
RUN pnpm install --frozen-lockfile
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/pnpm/store,id=pnpm",
			},
		},
		{
			name: "composer uses default cache path",
			content: `FROM php:8.4
RUN composer install --no-dev && composer clear-cache
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/composer,id=composer",
			},
			wantNotContains: []string{"composer clear-cache", "--mount=type=cache,target=/tmp/cache"},
			wantEditCount:   0,
		},
		{
			name: "uv no-cache and cleanup removed",
			content: `FROM python:3.13
RUN uv sync --no-cache --frozen && uv cache clean
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
				"uv sync --frozen",
			},
			wantNotContains: []string{"--no-cache", "uv cache clean"},
			wantEditCount:   0,
		},
		{
			name: "uv python install adds cache mount",
			content: `FROM python:3.13
RUN uv python install 3.12
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
			},
		},
		{
			name: "uv python install --no-cache flag stripped",
			content: `FROM python:3.13
RUN uv python install --no-cache 3.12
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
				"uv python install 3.12",
			},
			wantNotContains: []string{"--no-cache"},
			wantEditCount:   0,
		},
		{
			name: "UV_NO_CACHE env removed (sole variable)",
			content: `FROM python:3.13
ENV UV_NO_CACHE=1
RUN uv sync --frozen
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
			},
			wantNotContains: []string{"UV_NO_CACHE"},
			wantEditCount:   0,
		},
		{
			name: "UV_NO_CACHE env removed (multi-variable)",
			content: `FROM python:3.13
ENV UV_NO_CACHE=1 UV_LINK_MODE=copy
RUN uv sync --frozen
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
				"ENV UV_LINK_MODE=copy",
			},
			wantNotContains: []string{"UV_NO_CACHE"},
			wantEditCount:   0,
		},
		{
			name:    "UV_NO_CACHE env removed (sole variable, multiline ENV)",
			content: "FROM python:3.13\nENV \\\n    UV_NO_CACHE=1\nRUN uv sync --frozen\n",
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
			},
			wantNotContains: []string{"UV_NO_CACHE"},
			wantEditCount:   0,
		},
		{
			name:    "UV_NO_CACHE env removed (multi-variable with spaces in value)",
			content: "FROM python:3.13\nENV UV_NO_CACHE=1 MY_OPTS=\"-O2 -Wall\"\nRUN uv sync --frozen\n",
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/uv,id=uv",
				`ENV MY_OPTS="-O2 -Wall"`,
			},
			wantNotContains: []string{"UV_NO_CACHE"},
			wantEditCount:   0,
		},
		{
			name: "heredoc adds mount and removes cleanup line",
			content: `FROM node:20
RUN <<EOF
npm install
npm cache clean --force
EOF
`,
			wantFixContains: []string{"--mount=type=cache,target=/root/.npm,id=npm"},
			wantNotContains: []string{"npm cache clean"},
			wantEditCount:   0,
		},
		{
			name: "bun cleanup removed",
			content: `FROM oven/bun:1.2
RUN bun install --no-cache && bun pm cache rm
`,
			wantFixContains: []string{"--mount=type=cache,target=/root/.bun/install/cache,id=bun", "bun install"},
			wantNotContains: []string{"--no-cache", "bun pm cache rm"},
			wantEditCount:   0,
		},
		{
			name: "PIP_NO_CACHE_DIR env removed (sole variable)",
			content: `FROM python:3.13
ENV PIP_NO_CACHE_DIR=1
RUN pip install -r requirements.txt
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/pip,id=pip",
			},
			wantNotContains: []string{"PIP_NO_CACHE_DIR"},
			wantEditCount:   0,
		},
		{
			name:    "PIP_NO_CACHE_DIR env removed (multi-variable)",
			content: "FROM python:3.13\nENV PIP_NO_CACHE_DIR=1 PIP_INDEX_URL=https://example.com/simple\nRUN pip install -r requirements.txt\n",
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.cache/pip,id=pip",
				"ENV PIP_INDEX_URL=https://example.com/simple",
			},
			wantNotContains: []string{"PIP_NO_CACHE_DIR"},
			wantEditCount:   0,
		},
		{
			name: "npm_config_cache resolves cache path",
			content: `FROM node:20
ENV npm_config_cache=/tmp/npm-cache
RUN npm install
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/tmp/npm-cache,id=npm",
			},
		},
		{
			name: "npm_config_cache case insensitive (NPM_CONFIG_CACHE)",
			content: `FROM node:20
ENV NPM_CONFIG_CACHE=/opt/npm-cache
RUN npm ci
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/opt/npm-cache,id=npm",
			},
		},
		{
			name: "BUN_INSTALL_CACHE_DIR resolves cache path",
			content: `FROM oven/bun:1.2
ENV BUN_INSTALL_CACHE_DIR=/tmp/bun-cache
RUN bun install
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/tmp/bun-cache,id=bun",
			},
		},
		{
			name: "npm_config_cache relative path resolved against WORKDIR",
			content: `FROM node:20
WORKDIR /app
ENV npm_config_cache=.npm-cache
RUN npm install
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/app/.npm-cache,id=npm",
			},
		},
		{
			name: "npm_config_cache relative path resolved against default workdir",
			content: `FROM node:20
ENV npm_config_cache=npm-cache
RUN npm install
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/npm-cache,id=npm",
			},
		},
		{
			name: "npm_config_cache empty value uses default target",
			content: `FROM node:20
ENV npm_config_cache=
RUN npm install
`,
			wantFixContains: []string{
				"--mount=type=cache,target=/root/.npm,id=npm",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := r.Check(input)
			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			fix := violations[0].SuggestedFix
			if fix == nil {
				t.Fatal("expected suggested fix")
			}
			if fix.Safety != rules.FixSuggestion {
				t.Fatalf("fix safety = %v, want %v", fix.Safety, rules.FixSuggestion)
			}
			if fix.Priority != 90 {
				t.Fatalf("fix priority = %d, want 90", fix.Priority)
			}

			edits := resolveIfAsync(t, fix, tt.content)
			fixedContent := applyResolvedFix(t, fix, tt.content)

			// Only check edit count if explicitly set (non-zero)
			if tt.wantEditCount != 0 && len(edits) != tt.wantEditCount {
				t.Fatalf("fix edits = %d, want %d", len(edits), tt.wantEditCount)
			}

			for _, want := range tt.wantFixContains {
				if !strings.Contains(fixedContent, want) {
					t.Fatalf("fixed content missing %q in:\n%s", want, fixedContent)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(fixedContent, notWant) {
					t.Fatalf("fixed content unexpectedly contains %q in:\n%s", notWant, fixedContent)
				}
			}
		})
	}
}

func TestGoUsesDependencyCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{name: "go build", cmd: shell.CommandInfo{Subcommand: "build", Args: []string{"build", "./..."}}, want: true},
		{name: "go mod download", cmd: shell.CommandInfo{Subcommand: "mod", Args: []string{"mod", "download"}}, want: true},
		{
			name: "go generate with build arg",
			cmd:  shell.CommandInfo{Subcommand: "generate", Args: []string{"generate", "-run", "build"}},
			want: false,
		},
		{name: "go mod tidy", cmd: shell.CommandInfo{Subcommand: "mod", Args: []string{"mod", "tidy"}}, want: false},
		{name: "go test", cmd: shell.CommandInfo{Subcommand: "test", Args: []string{"test", "./..."}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := goUsesDependencyCache(tt.cmd)
			if got != tt.want {
				t.Fatalf("goUsesDependencyCache() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPreferPackageCacheMountsRule_FactsPathReusesRemainingEnvRemovalsAcrossRuns(t *testing.T) {
	t.Parallel()

	content := `FROM python:3.13
ENV PIP_NO_CACHE_DIR=1
RUN pip install -r requirements-dev.txt
RUN pip install -r requirements.txt
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferPackageCacheMountsRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}

	envRemovalEdits := 0
	for i, violation := range violations {
		if violation.SuggestedFix == nil {
			t.Fatalf("violation %d missing suggested fix", i)
		}
		for _, edit := range violation.SuggestedFix.Edits {
			if edit.Location.Start.Line == 2 {
				envRemovalEdits++
			}
		}
	}

	if envRemovalEdits != 1 {
		t.Fatalf("expected exactly 1 ENV removal edit across fixes, got %d", envRemovalEdits)
	}
}

func TestPreferPackageCacheMountsRule_FixHandlesIndentedRunInsertion(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20 AS base
	RUN echo base

FROM node:22 AS build
	RUN bun install
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferPackageCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	result, err := (&fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}).Apply(
		context.Background(),
		violations,
		map[string][]byte{"Dockerfile": []byte(content)},
	)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM alpine:3.20 AS base
	RUN echo base

FROM node:22 AS build
	RUN --mount=type=cache,target=/root/.bun/install/cache,id=bun bun install
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

// TestPreferPackageCacheMountsRule_WrappedInstallCleanupComposesWithDL3027 is a
// regression test ensuring that a multi-line install command (with backslash
// continuations within the command itself) followed by a cleanup command
// produces targeted cleanup edits, not a tail rewrite that would suppress
// DL3027 (apt → apt-get) on the same RUN instruction.
func TestPreferPackageCacheMountsRule_WrappedInstallCleanupComposesWithDL3027(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\nRUN apt-get install -y \\\n    wget \\\n    curl && apt-get clean\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferPackageCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected suggested fix")
	}

	// We should get targeted edits (mount insertion + cleanup deletion),
	// NOT a single tail rewrite covering the entire RUN body.
	if len(fix.Edits) < 2 {
		t.Fatalf("expected at least 2 edits (mount insertion + cleanup deletion), got %d", len(fix.Edits))
	}

	// First edit: zero-width mount insertion.
	ins := fix.Edits[0]
	if ins.Location.Start != ins.Location.End {
		t.Errorf("mount insertion should be zero-width, got %v-%v", ins.Location.Start, ins.Location.End)
	}

	// Cleanup edit should NOT start on line 2 (where the install command
	// begins), proving it doesn't overlap with DL3027 edits there.
	cleanup := fix.Edits[1]
	if cleanup.Location.Start.Line == 2 {
		t.Errorf("cleanup edit starts on line 2 (overlaps install); expected a targeted deletion on a later line")
	}
}

func TestPreferPackageCacheMountsRule_FactsPathRemovesAllRepeatedCacheDisablingEnvBindings(t *testing.T) {
	t.Parallel()

	content := `FROM python:3.13
ENV PIP_NO_CACHE_DIR=1
ENV PIP_NO_CACHE_DIR=1
RUN pip install -r requirements.txt
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferPackageCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	envRemovalEdits := 0
	for _, edit := range violations[0].SuggestedFix.Edits {
		if edit.NewText == "" && (edit.Location.Start.Line == 2 || edit.Location.Start.Line == 3) {
			envRemovalEdits++
		}
	}

	if envRemovalEdits != 2 {
		t.Fatalf("expected 2 ENV removal edits, got %d", envRemovalEdits)
	}
}

func TestPreferPackageCacheMountsResolver_ReidentifiesRunAfterEarlierRunRemoved(t *testing.T) {
	t.Parallel()

	original := `FROM python:3.13
RUN pip install --no-cache-dir left-pad
RUN pip install --no-cache-dir lodash
`
	input := testutil.MakeLintInput(t, "Dockerfile", original)
	violations := NewPreferPackageCacheMountsRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}

	fix := violations[1].SuggestedFix
	if fix == nil || !fix.NeedsResolve {
		t.Fatal("expected async suggested fix for second RUN")
	}

	modified := `FROM python:3.13
RUN pip install --no-cache-dir lodash
`
	got := applyResolvedFix(t, fix, modified)
	want := `FROM python:3.13
RUN --mount=type=cache,target=/root/.cache/pip,id=pip pip install lodash
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferPackageCacheMountsResolver_OnlyRemovesSelectedEnvBindings(t *testing.T) {
	t.Parallel()

	content := `FROM python:3.13
ENV PIP_NO_CACHE_DIR=1
RUN pip install --no-cache-dir left-pad
RUN pip install --no-cache-dir lodash
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferPackageCacheMountsRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}

	secondFix := violations[1].SuggestedFix
	if secondFix == nil || !secondFix.NeedsResolve {
		t.Fatal("expected async suggested fix for second RUN")
	}

	got := applyResolvedFix(t, secondFix, content)
	if !strings.Contains(got, "ENV PIP_NO_CACHE_DIR=1") {
		t.Fatalf("second async fix unexpectedly removed shared ENV binding:\n%s", got)
	}
	if !strings.Contains(got, "--mount=type=cache,target=/root/.cache/pip,id=pip") {
		t.Fatalf("second async fix did not rewrite the target RUN:\n%s", got)
	}
}

func TestUVUsesCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{name: "uv sync", cmd: shell.CommandInfo{Subcommand: "sync", Args: []string{"sync", "--frozen"}}, want: true},
		{
			name: "uv pip install",
			cmd: shell.CommandInfo{
				Subcommand: "pip",
				Args:       []string{"pip", "install", "-r", "requirements.txt"},
			},
			want: true,
		},
		{
			name: "uv tool install",
			cmd: shell.CommandInfo{
				Subcommand: "tool",
				Args:       []string{"tool", "install", "ruff"},
			},
			want: true,
		},
		{
			name: "uv python install",
			cmd: shell.CommandInfo{
				Subcommand: "python",
				Args:       []string{"python", "install", "3.13"},
			},
			want: true,
		},
		{name: "uv pip compile", cmd: shell.CommandInfo{Subcommand: "pip", Args: []string{"pip", "compile"}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := uvUsesCache(tt.cmd)
			if got != tt.want {
				t.Fatalf("uvUsesCache() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAptListCleanup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "simple apt lists cleanup", command: "rm -rf /var/lib/apt/lists/*", want: true},
		{name: "with mixed flags", command: "rm -fr /var/lib/apt/lists", want: true},
		{name: "multiple apt list paths", command: "rm -rf /var/lib/apt/lists/* /var/lib/apt/lists/partial", want: true},
		{name: "different path", command: "rm -rf /tmp/cache", want: false},
		{name: "missing force flag", command: "rm -r /var/lib/apt/lists/*", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isAptListCleanup(tt.command)
			if got != tt.want {
				t.Fatalf("isAptListCleanup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPackageCacheDirCleanup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		command  string
		cacheDir string
		want     bool
	}{
		{
			name:     "dnf cache cleanup",
			command:  "rm -rf /var/cache/dnf",
			cacheDir: "/var/cache/dnf",
			want:     true,
		},
		{
			name:     "yum cache cleanup with wildcard",
			command:  "rm -fr /var/cache/yum/*",
			cacheDir: "/var/cache/yum",
			want:     true,
		},
		{
			name:     "apk cache cleanup with wildcard",
			command:  "rm -rf /var/cache/apk/*",
			cacheDir: "/var/cache/apk",
			want:     true,
		},
		{
			name:     "zypper cache cleanup",
			command:  "rm -rf /var/cache/zypp/packages",
			cacheDir: "/var/cache/zypp",
			want:     true,
		},
		{
			name:     "separate recursive and force flags",
			command:  "rm -r -f /var/cache/dnf",
			cacheDir: "/var/cache/dnf",
			want:     true,
		},
		{
			name:     "different cache path",
			command:  "rm -rf /var/cache/dnf /tmp/cache",
			cacheDir: "/var/cache/dnf",
			want:     false,
		},
		{
			name:     "missing force flag",
			command:  "rm -r /var/cache/dnf",
			cacheDir: "/var/cache/dnf",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isPackageCacheDirCleanup(tt.command, tt.cacheDir)
			if got != tt.want {
				t.Fatalf("isPackageCacheDirCleanup() = %v, want %v", got, tt.want)
			}
		})
	}
}
