package fix

import (
	"context"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestDL4001CleanupResolver_ID(t *testing.T) {
	t.Parallel()
	r := &dl4001CleanupResolver{}
	if got := r.ID(); got != rules.DL4001CleanupResolverID {
		t.Errorf("ID() = %q, want %q", got, rules.DL4001CleanupResolverID)
	}
}

func TestDL4001CleanupResolver_PreservesMixedEnv(t *testing.T) {
	t.Parallel()

	// ENV binds a tool-config key (CURL_HOME) alongside unrelated keys
	// (APP_VERSION, PATH). Deleting the whole instruction would drop
	// APP_VERSION and PATH, which is data loss. The resolver must either
	// leave the instruction alone or surgically drop only CURL_HOME; the
	// current MVP chooses "leave it alone" and still removes the COPY
	// heredoc writing .curlrc. Fixture is post-sync — curl invocations
	// have already been rewritten to wget.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
ENV CURL_HOME=/etc/curl APP_VERSION=1.0 PATH=/usr/local/bin:$PATH
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry 5
EOF
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, e := range edits {
		// Lines 3 is the ENV with mixed keys — must not be deleted.
		if e.Location.Start.Line <= 3 && e.Location.End.Line > 3 && e.NewText == "" {
			t.Fatalf("cleanup deleted mixed ENV on line 3: edit=%+v", e)
		}
	}
}

func TestDL4001CleanupResolver_DeletesDedicatedEnv(t *testing.T) {
	t.Parallel()

	// An ENV that binds only a tool-config key is safe to delete entirely.
	// This fixture simulates the post-sync state where curl invocations have
	// already been rewritten to wget — the guard that blocks cleanup when
	// the tool is still invoked must not trip here.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
ENV CURL_HOME=/etc/curl
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	foundEnvDelete := false
	for _, e := range edits {
		if e.Location.Start.Line == 3 && e.Location.End.Line == 4 && e.NewText == "" {
			foundEnvDelete = true
			break
		}
	}
	if !foundEnvDelete {
		t.Fatalf("expected ENV-only line 3 to be deleted, edits=%+v", edits)
	}
}

func TestDL4001CleanupResolver_DoesNotMisalignHeredocEdits(t *testing.T) {
	t.Parallel()

	// Heredoc RUN: the install lives on the first line of the heredoc body,
	// which does NOT share a column origin with the "RUN <<EOF" line above.
	// Any edits the resolver emits must either target the right column or
	// skip the heredoc entirely — they must never reference a column offset
	// derived from the "RUN <<EOF" line. Fixture is post-sync: curl
	// invocations have already been rewritten to wget.
	dockerfile := `FROM ubuntu:22.04
RUN <<EOF
apt-get install -y curl wget
EOF
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// The install line inside the heredoc is line 3, column 0 ("apt-get ...").
	// A correct edit for "curl" (multi-package case) would target roughly
	// line 3, columns near "curl". An INCORRECT edit would either point at a
	// bogus column (cmdStartCol added from the RUN <<EOF header line) or
	// reference line 0/outside the file. Assert the heredoc isn't mauled.
	dockerfileLines := strings.Split(dockerfile, "\n")
	for _, e := range edits {
		if e.NewText != "" {
			continue
		}
		startLine := e.Location.Start.Line
		endLine := e.Location.End.Line
		if startLine < 1 || endLine < startLine {
			t.Fatalf("invalid edit range: %+v", e.Location)
		}
		if e.Location.Start.Line == 3 {
			line := dockerfileLines[2]
			startCol := e.Location.Start.Column
			endCol := e.Location.End.Column
			if startCol < 0 || endCol > len(line) || startCol > endCol {
				t.Fatalf(
					"edit column out of bounds for heredoc body line %q: %+v",
					line,
					e.Location,
				)
			}
			// Whatever slice we cut out must at least contain "curl" — otherwise
			// we'd be deleting the wrong bytes. Allow a leading space from the
			// "delete with leading space" branch of packageDeleteLocation.
			slice := line[startCol:endCol]
			if !strings.Contains(slice, "curl") {
				t.Fatalf("heredoc edit deletes %q, which does not contain 'curl'", slice)
			}
		}
	}
}

func TestDL4001CleanupResolver_RespectsBacktickEscape(t *testing.T) {
	t.Parallel()

	// Windows Dockerfile with backtick escape: a RUN instruction continues
	// across lines with backtick. The resolver must honor the escape
	// directive; otherwise line continuation won't be detected and the
	// reconstructed script will be wrong. Fixture is post-sync — curl
	// invocations have already been rewritten to wget.
	dockerfile := "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2025\n" +
		"SHELL [\"pwsh\", \"-Command\"]\n" +
		"RUN choco install -y `\n" +
		"    curl `\n" +
		"    wget\n" +
		"RUN wget https://example.com/two\n"

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// The choco install spans lines 4-6 with "curl" on line 5. A correct
	// cleanup edit deletes "curl" on line 5 (1-based). If the resolver
	// assumes backslash escape, it won't detect the continuation and will
	// miss the "curl" token inside the multi-line install entirely.
	foundCurlEdit := false
	dockerfileLines := strings.Split(dockerfile, "\n")
	for _, e := range edits {
		if e.NewText != "" {
			continue
		}
		if e.Location.Start.Line == 5 {
			line := dockerfileLines[4]
			if e.Location.End.Column > len(line) {
				t.Fatalf("edit extends past line 5: %+v, line=%q", e.Location, line)
			}
			slice := line[e.Location.Start.Column:e.Location.End.Column]
			if strings.Contains(slice, "curl") {
				foundCurlEdit = true
				break
			}
		}
	}
	if !foundCurlEdit {
		t.Fatalf("expected edit deleting 'curl' on line 5 of backtick-escaped RUN, edits=%+v", edits)
	}
}

func TestDL4001CleanupResolver_UsesPerRunShellVariant(t *testing.T) {
	t.Parallel()

	// Windows/PowerShell stage: the install uses PowerShell's statement
	// separator ";" (POSIX treats that as end-of-command), and the choco
	// install references curl twice — once with a quoted version, once with
	// a literal name. If the resolver parsed this under bash semantics,
	// PowerShell-specific tokens would be misread and we'd miss the "curl"
	// package token. Using the stage's effective shell variant ensures
	// FindInstallPackages walks the right grammar. Fixture is post-sync —
	// curl.exe invocations have already been rewritten to wget.exe.
	dockerfile := "FROM mcr.microsoft.com/windows/servercore:ltsc2025\n" +
		"SHELL [\"pwsh\", \"-Command\"]\n" +
		"RUN $ErrorActionPreference='Stop'; choco install -y curl wget ca-certificates\n" +
		"RUN wget.exe https://example.com/two\n"

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Expect an edit that deletes "curl" from the choco install on line 3.
	found := false
	dockerfileLines := strings.Split(dockerfile, "\n")
	for _, e := range edits {
		if e.NewText != "" || e.Location.Start.Line != 3 {
			continue
		}
		line := dockerfileLines[2]
		if e.Location.End.Column > len(line) {
			t.Fatalf("edit extends past line 3: %+v, line=%q", e.Location, line)
		}
		slice := line[e.Location.Start.Column:e.Location.End.Column]
		if strings.Contains(slice, "curl") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected edit deleting 'curl' from choco install on line 3, edits=%+v", edits)
	}
}

func TestDL4001CleanupResolver_PreservesInstallWhenInvocationsRemain(t *testing.T) {
	t.Parallel()

	// Simulates the post-sync state of a Dockerfile where the sync rewrites
	// couldn't cover every wget invocation (complex flags, shell substitution,
	// etc. fell outside the deterministic subset). wget is still called at
	// runtime, so we MUST NOT remove the wget install — that would turn the
	// build into "wget: command not found".
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
RUN wget --mirror --recursive https://example.com/stuff
RUN curl -fL -O https://example.com/file.tgz
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "wget"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(edits) != 0 {
		t.Fatalf("expected no edits (wget still invoked), got %d: %+v", len(edits), edits)
	}
}

func TestDL4001CleanupResolver_DropsInstallSubcommandInChain(t *testing.T) {
	t.Parallel()

	// The install has wget as its only package, but it sits inside a
	// chained RUN alongside unrelated commands (apt-get update, apt-get
	// clean). Deleting the whole RUN would drop those side effects; the
	// resolver must delete just the "apt-get install -y wget" segment.
	// Fixture is post-sync — wget invocations have already been rewritten
	// to curl.
	dockerfile := "FROM ubuntu:22.04\n" +
		"RUN apt-get update && apt-get install -y wget && apt-get clean\n" +
		"RUN curl https://example.com/one\n"

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "wget"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Apply edits via the production helper.
	got := ApplyEdits([]byte(dockerfile), edits)
	if strings.Contains(string(got), "apt-get install") {
		t.Fatalf("apt-get install should have been removed:\n%s", got)
	}
	if !strings.Contains(string(got), "apt-get update") {
		t.Fatalf("apt-get update should be preserved:\n%s", got)
	}
	if !strings.Contains(string(got), "apt-get clean") {
		t.Fatalf("apt-get clean should be preserved:\n%s", got)
	}
}

func TestDL4001CleanupResolver_DropsBareEnvVarCopy(t *testing.T) {
	t.Parallel()

	// COPY destination is a bare env-var reference (no /literal suffix).
	// "${WGETRC}" expands to the wget config file path at build time, so
	// the entire COPY is config for wget and must be removed when wget is
	// evicted. Without handling bare env-var destinations, the previous
	// code left this COPY in place and produced a broken image referring
	// to WGETRC after ENV WGETRC was deleted. Fixture is post-sync — wget
	// invocations have already been rewritten to curl.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
ENV WGETRC=/etc/wgetrc
COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
EOF
RUN curl https://example.com/one
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "wget"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Expect a whole-instruction delete covering the COPY heredoc (lines 4-6).
	found := false
	for _, e := range edits {
		if e.NewText != "" {
			continue
		}
		if e.Location.Start.Line == 4 && e.Location.End.Line >= 7 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected delete of COPY ${WGETRC} heredoc (lines 4-6), edits=%+v", edits)
	}
}

func TestDL4001CleanupResolver_DropsCurlrcCopy(t *testing.T) {
	t.Parallel()

	// Fixture is post-sync — curl invocations have already been rewritten to wget.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
COPY --chmod=0644 <<EOF /etc/.curlrc
--retry 5
EOF
RUN wget https://example.com/two
`

	r := &dl4001CleanupResolver{}
	edits, err := r.Resolve(
		context.Background(),
		ResolveContext{FilePath: "Dockerfile", Content: []byte(dockerfile)},
		&rules.SuggestedFix{
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: "curl"},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// At least one edit should delete lines covering the COPY heredoc (lines 3-5).
	found := false
	for _, e := range edits {
		if e.NewText != "" {
			continue
		}
		if e.Location.Start.Line == 3 && e.Location.End.Line >= 6 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected curlrc COPY heredoc (lines 3-5) deletion, edits=%+v", edits)
	}
}
