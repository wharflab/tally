package facts

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

type countingContextReader struct {
	files        map[string]string
	ignoredPaths map[string]bool
	heredocPaths map[string]bool
	reads        map[string]int
}

func (c *countingContextReader) FileExists(path string) bool {
	_, ok := c.files[path]
	return ok
}

func (c *countingContextReader) ReadFile(path string) ([]byte, error) {
	if c.reads == nil {
		c.reads = make(map[string]int)
	}
	c.reads[path]++
	content, ok := c.files[path]
	if !ok {
		return nil, fmt.Errorf("missing file %q", path)
	}
	return []byte(content), nil
}

func (c *countingContextReader) IsIgnored(path string) (bool, error) {
	return c.ignoredPaths[path], nil
}

func (c *countingContextReader) IsHeredocFile(path string) bool {
	return c.heredocPaths[path]
}

func TestFileFacts_BuildsRunFactsWithEnvShellAndCommands(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `# hadolint shell=bash
FROM alpine:3.20
ENV DEBIAN_FRONTEND=noninteractive npm_config_cache=.npm
WORKDIR /app
RUN env PIP_INDEX_URL=https://example.com/simple pip install flask && npm install express
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if stage.InitialShell.Variant != shell.VariantBash {
		t.Fatalf("expected initial shell variant %v, got %v", shell.VariantBash, stage.InitialShell.Variant)
	}
	if len(stage.Runs) != 1 {
		t.Fatalf("expected 1 RUN fact, got %d", len(stage.Runs))
	}

	run := stage.Runs[0]
	if run.Workdir != "/app" {
		t.Fatalf("expected workdir /app, got %q", run.Workdir)
	}
	if !run.Env.AptNonInteractive {
		t.Fatal("expected DEBIAN_FRONTEND=noninteractive to be reflected in env facts")
	}
	if got := run.CachePathOverrides["npm"]; got != "/app/.npm" {
		t.Fatalf("expected npm cache override /app/.npm, got %q", got)
	}
	if len(run.CommandInfos) != 3 {
		t.Fatalf("expected 3 command facts (env, pip, npm), got %d", len(run.CommandInfos))
	}
	if run.CommandInfos[0].Name != "env" || run.CommandInfos[1].Name != "pip" || run.CommandInfos[2].Name != "npm" {
		t.Fatalf("unexpected command sequence: %#v", run.CommandInfos)
	}
	if len(run.InstallCommands) != 2 {
		t.Fatalf("expected 2 install commands, got %d", len(run.InstallCommands))
	}
}

func TestResolveWorkdirAndUnquote(t *testing.T) {
	t.Parallel()

	if got := ResolveWorkdir("/app", "tmp/cache"); got != "/app/tmp/cache" {
		t.Fatalf("ResolveWorkdir() relative = %q, want %q", got, "/app/tmp/cache")
	}
	if got := ResolveWorkdir("/app", "/var/cache"); got != "/var/cache" {
		t.Fatalf("ResolveWorkdir() absolute = %q, want %q", got, "/var/cache")
	}
	if got := Unquote(`"quoted"`); got != "quoted" {
		t.Fatalf("Unquote() double-quoted = %q, want %q", got, "quoted")
	}
	if got := Unquote("'single'"); got != "single" {
		t.Fatalf("Unquote() single-quoted = %q, want %q", got, "single")
	}
	if got := Unquote("bare"); got != "bare" {
		t.Fatalf("Unquote() bare = %q, want %q", got, "bare")
	}
}

func TestFileFacts_PowerShellErrorModeIsTrackedPerRun(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM mcr.microsoft.com/powershell:nanoserver-ltsc2022
SHELL ["powershell","-Command","Write-Host hi"]
RUN npm install left-pad
SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; Write-Host hi"]
RUN npm install lodash
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if stage.BaseImageOS != semantic.BaseImageOSWindows {
		t.Fatalf("expected windows base image, got %v", stage.BaseImageOS)
	}
	if len(stage.Runs) != 2 {
		t.Fatalf("expected 2 RUN facts, got %d", len(stage.Runs))
	}
	if !stage.Runs[0].Shell.IsPowerShell || !stage.Runs[0].Shell.PowerShellMayMaskErr {
		t.Fatal("expected first RUN to inherit masking PowerShell shell facts")
	}
	if !stage.Runs[1].Shell.IsPowerShell || stage.Runs[1].Shell.PowerShellMayMaskErr {
		t.Fatal("expected second RUN to inherit PowerShell shell facts with stop behavior")
	}
}

func TestFileFacts_CacheDisablingEnvTracksAllBindingsForSameKey(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM python:3.13
ENV PIP_NO_CACHE_DIR=1
ENV PIP_NO_CACHE_DIR=1
RUN pip install -r requirements.txt
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if len(stage.Runs) != 1 {
		t.Fatalf("expected 1 RUN fact, got %d", len(stage.Runs))
	}

	run := stage.Runs[0]
	if len(run.CacheDisablingEnv) != 2 {
		t.Fatalf("expected 2 cache-disabling bindings, got %d", len(run.CacheDisablingEnv))
	}
	if run.CacheDisablingEnv[0].Key != "PIP_NO_CACHE_DIR" || run.CacheDisablingEnv[1].Key != "PIP_NO_CACHE_DIR" {
		t.Fatalf("unexpected cache-disabling bindings: %#v", run.CacheDisablingEnv)
	}
}

func TestIsRootUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		user string
		want bool
	}{
		{"root", true},
		{"ROOT", true},
		{"Root", true},
		{"0", true},
		{"root:root", true},
		{"0:0", true},
		{"root:wheel", true},
		{"0:wheel", true},
		{"appuser", false},
		{"1000", false},
		{"appuser:appgroup", false},
		{"1000:1000", false},
		{"  root  ", true},
		{"nobody", false},
		{"www-data", false},
	}

	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			t.Parallel()
			if got := IsRootUser(tt.user); got != tt.want {
				t.Errorf("IsRootUser(%q) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

func TestFileFacts_UserAndVolumeTracking(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM ubuntu:22.04
WORKDIR /app
USER root
RUN apt-get update
USER appuser:appgroup
VOLUME /data /var/lib/db
VOLUME /var/log/app
CMD ["app"]
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if stage.EffectiveUser != "appuser:appgroup" {
		t.Fatalf("EffectiveUser = %q, want %q", stage.EffectiveUser, "appuser:appgroup")
	}
	if len(stage.UserCommands) != 2 {
		t.Fatalf("UserCommands count = %d, want 2", len(stage.UserCommands))
	}
	if stage.UserCommands[0].User != "root" || stage.UserCommands[1].User != "appuser:appgroup" {
		t.Fatalf("unexpected UserCommands: %v, %v", stage.UserCommands[0].User, stage.UserCommands[1].User)
	}
	wantVolumes := []string{"/data", "/var/lib/db", "/var/log/app"}
	if len(stage.Volumes) != len(wantVolumes) {
		t.Fatalf("Volumes count = %d, want %d", len(stage.Volumes), len(wantVolumes))
	}
	for i, v := range stage.Volumes {
		if v != wantVolumes[i] {
			t.Fatalf("Volumes[%d] = %q, want %q", i, v, wantVolumes[i])
		}
	}
	if stage.FinalWorkdir != "/app" {
		t.Fatalf("FinalWorkdir = %q, want %q", stage.FinalWorkdir, "/app")
	}
}

func TestFileFacts_PrivilegeDropEntrypoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		content       string
		stageIdx      int  // stage to check (default 0)
		wantEntryDrop bool // HasPrivilegeDropEntrypoint
		wantCmdDrop   bool // HasPrivilegeDropCmd
		wantHasEP     bool // HasEntrypoint
		wantDrops     bool // DropsPrivilegesAtRuntime
	}{
		{
			name: "gosu in ENTRYPOINT",
			content: `FROM ubuntu:22.04
ENTRYPOINT ["gosu", "postgres", "docker-entrypoint.sh"]
`,
			wantEntryDrop: true, wantHasEP: true, wantDrops: true,
		},
		{
			name: "su-exec in ENTRYPOINT",
			content: `FROM alpine:3.20
ENTRYPOINT ["su-exec", "redis", "redis-server"]
`,
			wantEntryDrop: true, wantHasEP: true, wantDrops: true,
		},
		{
			name: "gosu in CMD without ENTRYPOINT",
			content: `FROM ubuntu:22.04
CMD ["gosu", "nobody", "/app"]
`,
			wantCmdDrop: true, wantDrops: true,
		},
		{
			name: "gosu in CMD with ENTRYPOINT does not suppress",
			content: `FROM ubuntu:22.04
ENTRYPOINT ["/app"]
CMD ["gosu", "nobody"]
`,
			wantCmdDrop: true, wantHasEP: true, wantDrops: false,
		},
		{
			name: "docker-entrypoint.sh in CMD is not a tool",
			content: `FROM ubuntu:22.04
CMD ["docker-entrypoint.sh", "mysqld"]
`,
			wantDrops: false,
		},
		{
			name: "setpriv in ENTRYPOINT",
			content: `FROM ubuntu:22.04
ENTRYPOINT ["setpriv", "--reuid=1000", "--", "/app"]
`,
			wantEntryDrop: true, wantHasEP: true, wantDrops: true,
		},
		{
			name: "shell-form ENTRYPOINT with gosu",
			content: `FROM ubuntu:22.04
ENTRYPOINT exec gosu postgres "$@"
`,
			wantEntryDrop: true, wantHasEP: true, wantDrops: true,
		},
		{
			name: "entrypoint.sh script is not a tool",
			content: `FROM ubuntu:22.04
ENTRYPOINT ["/entrypoint.sh"]
`,
			wantHasEP: true, wantDrops: false,
		},
		{
			name: "regular ENTRYPOINT no priv drop",
			content: `FROM ubuntu:22.04
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantHasEP: true, wantDrops: false,
		},
		{
			name: "no ENTRYPOINT or CMD",
			content: `FROM ubuntu:22.04
RUN echo hello
`,
			wantDrops: false,
		},
		{
			name: "later ENTRYPOINT overrides gosu ENTRYPOINT",
			content: `FROM ubuntu:22.04
ENTRYPOINT ["gosu", "postgres", "start"]
ENTRYPOINT ["/app"]
`,
			wantHasEP: true, wantDrops: false,
		},
		{
			name: "later CMD overrides gosu CMD",
			content: `FROM ubuntu:22.04
CMD ["gosu", "nobody", "/app"]
CMD ["serve"]
`,
			wantDrops: false,
		},
		{
			name:     "inherited gosu ENTRYPOINT from parent stage",
			stageIdx: 1,
			content: `FROM ubuntu:22.04 AS base
ENTRYPOINT ["gosu", "postgres", "start"]

FROM base
CMD ["postgres"]
`,
			wantEntryDrop: true, wantHasEP: true, wantDrops: true,
		},
		{
			name:     "child overrides inherited gosu ENTRYPOINT",
			stageIdx: 1,
			content: `FROM ubuntu:22.04 AS base
ENTRYPOINT ["gosu", "postgres", "start"]

FROM base
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantHasEP: true, wantDrops: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ff := makeFileFacts(t, tt.content)
			stage := ff.Stage(tt.stageIdx)
			if stage == nil {
				t.Fatal("expected stage facts")
			}
			if stage.HasPrivilegeDropEntrypoint != tt.wantEntryDrop {
				t.Errorf("HasPrivilegeDropEntrypoint = %v, want %v", stage.HasPrivilegeDropEntrypoint, tt.wantEntryDrop)
			}
			if stage.HasPrivilegeDropCmd != tt.wantCmdDrop {
				t.Errorf("HasPrivilegeDropCmd = %v, want %v", stage.HasPrivilegeDropCmd, tt.wantCmdDrop)
			}
			if stage.HasEntrypoint != tt.wantHasEP {
				t.Errorf("HasEntrypoint = %v, want %v", stage.HasEntrypoint, tt.wantHasEP)
			}
			if stage.DropsPrivilegesAtRuntime() != tt.wantDrops {
				t.Errorf("DropsPrivilegesAtRuntime() = %v, want %v", stage.DropsPrivilegesAtRuntime(), tt.wantDrops)
			}
		})
	}
}

func TestStageFacts_FileContent_LazyContextRead(t *testing.T) {
	t.Parallel()

	ctx := &countingContextReader{
		files: map[string]string{
			"entrypoint.sh": "#!/bin/sh\nexec gosu app \"$@\"\n",
		},
	}

	fileFacts := makeFileFactsWithContext(t, `FROM ubuntu:22.04
COPY entrypoint.sh /app/entrypoint.sh
`, ctx)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if ctx.reads["entrypoint.sh"] != 0 {
		t.Fatalf("context file read during build = %d, want 0", ctx.reads["entrypoint.sh"])
	}

	content, ok := stage.FileContent("/app/entrypoint.sh")
	if !ok {
		t.Fatal("expected observable file content")
	}
	if !strings.Contains(content, "gosu") {
		t.Fatalf("FileContent() = %q, want gosu script", content)
	}
	if ctx.reads["entrypoint.sh"] != 1 {
		t.Fatalf("context file reads after first lookup = %d, want 1", ctx.reads["entrypoint.sh"])
	}

	content, ok = stage.FileContent("/app/entrypoint.sh")
	if !ok || !strings.Contains(content, "gosu") {
		t.Fatal("expected cached content on second lookup")
	}
	if ctx.reads["entrypoint.sh"] != 1 {
		t.Fatalf("context file reads after cached lookup = %d, want 1", ctx.reads["entrypoint.sh"])
	}
}

func TestStageFacts_FileContent_ComposesKnownAppend(t *testing.T) {
	t.Parallel()

	ctx := &countingContextReader{
		files: map[string]string{
			"entrypoint.sh": "#!/bin/sh\n",
		},
	}

	fileFacts := makeFileFactsWithContext(t, `FROM ubuntu:22.04
COPY entrypoint.sh /app/entrypoint.sh
RUN echo 'exec su-exec app "$@"' >> /app/entrypoint.sh
`, ctx)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}

	content, ok := stage.FileContent("/app/entrypoint.sh")
	if !ok {
		t.Fatal("expected composed observable content")
	}
	if !strings.Contains(content, "#!/bin/sh") || !strings.Contains(content, "su-exec") {
		t.Fatalf("FileContent() = %q, want combined base + append", content)
	}
}

func TestStageFacts_FileContent_UnknownBaseStaysUnobservable(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM ubuntu:22.04
COPY entrypoint.sh /app/entrypoint.sh
RUN echo 'exec su-exec app "$@"' >> /app/entrypoint.sh
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}

	if _, ok := stage.FileContent("/app/entrypoint.sh"); ok {
		t.Fatal("expected unknown base file content to stay unobservable after append")
	}
}

func TestStageFacts_FileContent_IgnoredContextSourceStaysUnobservable(t *testing.T) {
	t.Parallel()

	ctx := &countingContextReader{
		files: map[string]string{
			"entrypoint.sh": "#!/bin/sh\nexec gosu app \"$@\"\n",
		},
		ignoredPaths: map[string]bool{
			"entrypoint.sh": true,
		},
	}

	fileFacts := makeFileFactsWithContext(t, `FROM ubuntu:22.04
COPY entrypoint.sh /app/entrypoint.sh
`, ctx)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}

	if _, ok := stage.FileContent("/app/entrypoint.sh"); ok {
		t.Fatal("expected ignored context source to stay unobservable")
	}
	if ctx.reads["entrypoint.sh"] != 0 {
		t.Fatalf("ignored context source should not be read, got %d reads", ctx.reads["entrypoint.sh"])
	}
}

func TestStageFacts_FileContent_LocalStageCopyPreservesObservability(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM ubuntu:22.04 AS builder
COPY <<'EOF' /docker-entrypoint.sh
#!/bin/sh
exec gosu app "$@"
EOF

FROM ubuntu:22.04
COPY --from=builder /docker-entrypoint.sh /docker-entrypoint.sh
`)

	stage := fileFacts.Stage(1)
	if stage == nil {
		t.Fatal("expected final stage facts")
	}

	content, ok := stage.FileContent("/docker-entrypoint.sh")
	if !ok {
		t.Fatal("expected local stage copy to stay observable")
	}
	if !strings.Contains(content, "gosu") {
		t.Fatalf("FileContent() = %q, want copied script content", content)
	}
}

func TestStageFacts_FileContent_LocalStageCopyRespectsInheritedWorkdir(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM ubuntu:22.04 AS builder
WORKDIR /app
COPY <<'EOF' ./docker-entrypoint.sh
#!/bin/sh
exec gosu app "$@"
EOF

FROM builder
COPY --from=builder ./docker-entrypoint.sh ./docker-entrypoint.sh
ENTRYPOINT ["./docker-entrypoint.sh"]
`)

	stage := fileFacts.Stage(1)
	if stage == nil {
		t.Fatal("expected final stage facts")
	}
	if stage.FinalWorkdir != "/app" {
		t.Fatalf("FinalWorkdir = %q, want %q", stage.FinalWorkdir, "/app")
	}

	content, ok := stage.FileContent("/app/docker-entrypoint.sh")
	if !ok {
		t.Fatal("expected inherited workdir copy to stay observable at /app/docker-entrypoint.sh")
	}
	if !strings.Contains(content, "gosu") {
		t.Fatalf("FileContent() = %q, want copied script content", content)
	}
	if !stage.HasPrivilegeDropEntrypoint {
		t.Fatal("expected inherited workdir entrypoint lookup to detect privilege-drop script")
	}
}

func TestStageFacts_FileContent_AddLocalContextFileIsObservable(t *testing.T) {
	t.Parallel()

	ctx := &countingContextReader{
		files: map[string]string{
			"entrypoint.sh": "#!/bin/sh\nexec gosu app \"$@\"\n",
		},
	}

	fileFacts := makeFileFactsWithContext(t, `FROM ubuntu:22.04
ADD entrypoint.sh /docker-entrypoint.sh
`, ctx)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}

	content, ok := stage.FileContent("/docker-entrypoint.sh")
	if !ok {
		t.Fatal("expected ADD local file to stay observable")
	}
	if !strings.Contains(content, "gosu") {
		t.Fatalf("FileContent() = %q, want added script content", content)
	}
}

func TestStageFacts_FileContent_AddLocalArchiveStaysUnobservable(t *testing.T) {
	t.Parallel()

	ctx := &countingContextReader{
		files: map[string]string{
			"archive.tar.gz": "not actually read",
		},
	}

	fileFacts := makeFileFactsWithContext(t, `FROM ubuntu:22.04
ADD archive.tar.gz /opt/
`, ctx)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}

	if _, ok := stage.FileContent("/opt/archive.tar.gz"); ok {
		t.Fatal("expected ADD archive to stay unobservable")
	}
}

func TestFileFacts_PrivilegeDropEntrypointFromObservableFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		context  ContextFileReader
		wantDrop bool
	}{
		{
			name: "copy heredoc script",
			content: `FROM ubuntu:22.04
COPY <<'EOF' /docker-entrypoint.sh
#!/bin/sh
exec gosu postgres "$@"
EOF
ENTRYPOINT ["/docker-entrypoint.sh"]
`,
			wantDrop: true,
		},
		{
			name: "run-created script",
			content: `FROM ubuntu:22.04
RUN cat <<'EOF' > /docker-entrypoint.sh
#!/bin/sh
exec su-exec postgres "$@"
EOF
ENTRYPOINT ["/docker-entrypoint.sh"]
`,
			wantDrop: true,
		},
		{
			name: "context-backed script",
			content: `FROM ubuntu:22.04
COPY docker-entrypoint.sh /docker-entrypoint.sh
ENTRYPOINT ["/docker-entrypoint.sh"]
`,
			context: &countingContextReader{
				files: map[string]string{
					"docker-entrypoint.sh": "#!/bin/sh\nexec gosu postgres \"$@\"\n",
				},
			},
			wantDrop: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fileFacts := makeFileFactsWithContext(t, tt.content, tt.context)
			stage := fileFacts.Stage(0)
			if stage == nil {
				t.Fatal("expected stage facts")
			}
			if stage.HasPrivilegeDropEntrypoint != tt.wantDrop {
				t.Fatalf("HasPrivilegeDropEntrypoint = %v, want %v", stage.HasPrivilegeDropEntrypoint, tt.wantDrop)
			}
			if stage.DropsPrivilegesAtRuntime() != tt.wantDrop {
				t.Fatalf("DropsPrivilegesAtRuntime() = %v, want %v", stage.DropsPrivilegesAtRuntime(), tt.wantDrop)
			}
		})
	}
}

func TestFileFacts_MultiStageUserIsolation(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM ubuntu:22.04 AS builder
USER root
WORKDIR /build
VOLUME /build-cache

FROM alpine:3.20
USER 1000
WORKDIR /app
VOLUME /data
`)

	builder := fileFacts.Stage(0)
	if builder == nil {
		t.Fatal("expected builder stage facts")
	}
	if builder.EffectiveUser != "root" {
		t.Fatalf("builder EffectiveUser = %q, want %q", builder.EffectiveUser, "root")
	}
	if builder.FinalWorkdir != "/build" {
		t.Fatalf("builder FinalWorkdir = %q, want %q", builder.FinalWorkdir, "/build")
	}
	if len(builder.Volumes) != 1 || builder.Volumes[0] != "/build-cache" {
		t.Fatalf("builder Volumes = %v, want [/build-cache]", builder.Volumes)
	}
	if builder.IsLast {
		t.Fatal("builder should not be last stage")
	}

	runtime := fileFacts.Stage(1)
	if runtime == nil {
		t.Fatal("expected runtime stage facts")
	}
	if runtime.EffectiveUser != "1000" {
		t.Fatalf("runtime EffectiveUser = %q, want %q", runtime.EffectiveUser, "1000")
	}
	if runtime.FinalWorkdir != "/app" {
		t.Fatalf("runtime FinalWorkdir = %q, want %q", runtime.FinalWorkdir, "/app")
	}
	if len(runtime.Volumes) != 1 || runtime.Volumes[0] != "/data" {
		t.Fatalf("runtime Volumes = %v, want [/data]", runtime.Volumes)
	}
	if !runtime.IsLast {
		t.Fatal("runtime should be last stage")
	}
}

func TestFileFacts_NoUserInstruction(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM ubuntu:22.04
RUN echo hello
VOLUME /data
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if stage.EffectiveUser != "" {
		t.Fatalf("EffectiveUser = %q, want empty", stage.EffectiveUser)
	}
	if len(stage.UserCommands) != 0 {
		t.Fatalf("UserCommands count = %d, want 0", len(stage.UserCommands))
	}
	if stage.FinalWorkdir != "/" {
		t.Fatalf("FinalWorkdir = %q, want %q", stage.FinalWorkdir, "/")
	}
}

func makeFileFacts(t *testing.T, content string) *FileFacts {
	t.Helper()
	return makeFileFactsWithContext(t, content, nil)
}

func makeFileFactsWithContext(t *testing.T, content string, contextFiles ContextFileReader) *FileFacts {
	t.Helper()

	const file = "Dockerfile"

	parseResult, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("parse dockerfile: %v", err)
	}

	shellDirectives := parseTestShellDirectives(content)
	sem := semantic.NewBuilder(parseResult, nil, file).
		WithShellDirectives(toSemanticShellDirectives(shellDirectives)).
		Build()

	return NewFileFacts(
		file,
		parseResult,
		sem,
		shellDirectives,
		contextFiles,
	)
}

var testShellDirectivePattern = regexp.MustCompile(`(?i)^#\s*(?:tally|hadolint)\s+shell\s*=\s*([A-Za-z0-9_./-]+)\s*$`)

func parseTestShellDirectives(content string) []ShellDirective {
	lines := strings.Split(content, "\n")
	var directives []ShellDirective

	for i, line := range lines {
		matches := testShellDirectivePattern.FindStringSubmatch(strings.TrimSpace(line))
		if matches == nil {
			continue
		}
		directives = append(directives, ShellDirective{
			Line:  i,
			Shell: strings.ToLower(matches[1]),
		})
	}

	return directives
}

func toSemanticShellDirectives(directives []ShellDirective) []semantic.ShellDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]semantic.ShellDirective, 0, len(directives))
	for _, d := range directives {
		out = append(out, semantic.ShellDirective{
			Line:  d.Line,
			Shell: d.Shell,
		})
	}
	return out
}
