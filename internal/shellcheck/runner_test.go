package shellcheck

import (
	"context"
	"strings"
	"testing"
)

func TestRuntimeInitContext_DetachesCancellation(t *testing.T) {
	t.Parallel()

	type keyType string
	const key keyType = "k"

	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key, "v"))
	initCtx := runtimeInitContext(parent)
	cancel()

	if err := initCtx.Err(); err != nil {
		t.Fatalf("expected init context without cancellation, got err=%v", err)
	}
	if got := initCtx.Value(key); got != "v" {
		t.Fatalf("init context value=%v, want v", got)
	}
}

// TestReactorBasic exercises a single sc_check call through the Runner.
// This validates the WASM reactor module loads, hs_init succeeds, and
// sc_check returns valid JSON without proc_exit.
func TestReactorBasic(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	out, stderr, err := r.Run(ctx, "#!/bin/sh\necho $1\n", Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v (stderr=%q)", err, stderr)
	}

	// Should contain SC2086 (unquoted variable).
	var found bool
	for _, c := range out.Comments {
		if c.Code == 2086 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SC2086 in comments, got %+v", out.Comments)
	}
}

// TestReactorCleanScript verifies that a script with no issues returns empty comments.
func TestReactorCleanScript(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	out, _, err := r.Run(ctx, "#!/bin/sh\necho hello\n", Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(out.Comments) != 0 {
		t.Errorf("expected 0 comments for clean script, got %d: %+v", len(out.Comments), out.Comments)
	}
}

// TestReactorMultipleCalls verifies that the same runner can handle multiple sequential calls.
func TestReactorMultipleCalls(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	for i := range 5 {
		out, _, err := r.Run(ctx, "#!/bin/sh\necho $1\n", Options{
			Dialect:  "sh",
			Severity: "style",
			Norc:     true,
		})
		if err != nil {
			t.Fatalf("call %d: Run() error: %v", i, err)
		}
		var found bool
		for _, c := range out.Comments {
			if c.Code == 2086 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("call %d: expected SC2086", i)
		}
	}
}

// TestReactorStderrAlwaysEmpty confirms reactor returns empty stderr.
func TestReactorStderrAlwaysEmpty(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	_, stderr, err := r.Run(ctx, "#!/bin/sh\necho $1\n", Options{Norc: true})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got %q", stderr)
	}
}

// TestReactorFix verifies that the reactor returns fix suggestions.
func TestReactorFix(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	out, _, err := r.Run(ctx, "#!/bin/sh\necho $1\n", Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, c := range out.Comments {
		if c.Code == 2086 {
			if c.Fix == nil {
				t.Error("SC2086 should have a fix suggestion")
			}
			return
		}
	}
	t.Error("expected SC2086 with fix in comments")
}

// TestReactorBashDialect verifies that bash-specific constructs don't trigger
// posix-only warnings.
func TestReactorBashDialect(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	out, _, err := r.Run(ctx, "#!/bin/bash\narr=(one two three)\necho \"${arr[@]}\"\n", Options{
		Dialect:  "bash",
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	for _, c := range out.Comments {
		if c.Code == 3030 {
			t.Errorf("SC3030 should not fire for bash dialect, got: %+v", c)
		}
	}
}

// TestReactorExclude verifies the exclude option filters out specific codes.
func TestReactorExclude(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	out, _, err := r.Run(ctx, "#!/bin/sh\necho $1\n", Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
		Exclude:  []string{"2086"},
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	for _, c := range out.Comments {
		if c.Code == 2086 {
			t.Errorf("SC2086 should have been excluded, got: %+v", c)
		}
	}
}

// TestReactorInclude verifies the include option restricts to specific codes.
func TestReactorInclude(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	// Script that triggers SC2086 and SC2059.
	out, _, err := r.Run(ctx, "#!/bin/sh\nprintf $1\n", Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
		Include:  []string{"2086"},
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	for _, c := range out.Comments {
		if c.Code != 2086 {
			t.Errorf("only SC2086 should appear with include filter, got SC%d", c.Code)
		}
	}
}

// TestReactorJSON1Format verifies the reactor JSON output matches json1 structure
// by checking a known message string.
func TestReactorJSON1Format(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	out, _, err := r.Run(ctx, "#!/bin/sh\necho $1\n", Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, c := range out.Comments {
		if c.Code == 2086 {
			if !strings.Contains(c.Message, "Double quote") {
				t.Errorf("unexpected SC2086 message: %q", c.Message)
			}
			if c.File != "-" {
				t.Errorf("expected file=\"-\", got %q", c.File)
			}
			if c.Level != "info" {
				t.Errorf("expected level=\"info\", got %q", c.Level)
			}
			return
		}
	}
	t.Error("SC2086 not found")
}

// TestReactorConsistency verifies that identical scripts produce identical results
// across sequential calls on the same runner.
func TestReactorConsistency(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	script := "#!/bin/sh\necho $1\n"
	opts := Options{Dialect: "sh", Severity: "style", Norc: true, Exclude: []string{"1040"}}

	var firstLen int
	for i := range 10 {
		out, _, err := r.Run(ctx, script, opts)
		if err != nil {
			t.Fatalf("call %d: Run() error: %v", i, err)
		}
		if i == 0 {
			firstLen = len(out.Comments)
			if firstLen == 0 {
				t.Fatal("expected at least one comment from first call")
			}
		}
		if len(out.Comments) != firstLen {
			t.Fatalf("call %d: got %d comments, want %d (first call)", i, len(out.Comments), firstLen)
		}
	}
}

// TestReactorVersion verifies that the embedded WASM module exports
// sc_version and returns a valid ShellCheck version string.
func TestReactorVersion(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	ver, err := r.Version(ctx)
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	if ver == "" {
		t.Fatal("expected non-empty version string")
	}
	t.Logf("shellcheck version: %s", ver)
}

func TestBuildOpts(t *testing.T) {
	t.Parallel()

	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "empty options",
			opts: Options{},
			want: "",
		},
		{
			name: "dialect only",
			opts: Options{Dialect: "bash"},
			want: "dialect bash\n",
		},
		{
			name: "severity only",
			opts: Options{Severity: "warning"},
			want: "severity warning\n",
		},
		{
			name: "norc",
			opts: Options{Norc: true},
			want: "norc\n",
		},
		{
			name: "extended analysis true",
			opts: Options{ExtendedAnalysis: boolPtr(true)},
			want: "extended-analysis\n",
		},
		{
			name: "extended analysis false",
			opts: Options{ExtendedAnalysis: boolPtr(false)},
			want: "",
		},
		{
			name: "enable optional",
			opts: Options{EnableOptional: []string{"avoid-nullary-conditions", "require-double-brackets"}},
			want: "enable avoid-nullary-conditions\nenable require-double-brackets\n",
		},
		{
			name: "include codes",
			opts: Options{Include: []string{"2086", "2034"}},
			want: "include 2086\ninclude 2034\n",
		},
		{
			name: "include codes with SC prefix",
			opts: Options{Include: []string{"SC2086"}},
			want: "include 2086\n",
		},
		{
			name: "exclude codes",
			opts: Options{Exclude: []string{"1091", "2154"}},
			want: "exclude 1091\nexclude 2154\n",
		},
		{
			name: "exclude codes with SC prefix",
			opts: Options{Exclude: []string{"SC1040", "SC1091"}},
			want: "exclude 1040\nexclude 1091\n",
		},
		{
			name: "all options",
			opts: Options{
				Dialect:          "sh",
				Severity:         "error",
				Norc:             true,
				ExtendedAnalysis: boolPtr(true),
				EnableOptional:   []string{"require-double-brackets"},
				Include:          []string{"2086"},
				Exclude:          []string{"SC1091"},
			},
			want: "dialect sh\nseverity error\nnorc\nextended-analysis\nenable require-double-brackets\ninclude 2086\nexclude 1091\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildOpts(tt.opts)
			if got != tt.want {
				t.Errorf("buildOpts() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// TestReactorSingleInstanceDifferentScripts ensures the reactor can be reused across
// varied inputs without re-instantiation. This is the core property we need to
// make embedded ShellCheck fast enough for many snippets.
func TestReactorSingleInstanceDifferentScripts(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	prelude := "#!/bin/sh\n" +
		"export FTP_PROXY=1\nexport HTTPS_PROXY=1\nexport HTTP_PROXY=1\n" +
		"export NO_PROXY=1\nexport PATH=1\nexport ftp_proxy=1\n" +
		"export http_proxy=1\nexport https_proxy=1\nexport no_proxy=1\n"

	scripts := []string{
		prelude + "           echo $1",              // 198 bytes
		prelude + "    echo foo \\\n    && echo $1", // 209 bytes
		prelude + "            echo $1",             // 199 bytes
		prelude + "    echo $1",                     // 191 bytes
		prelude + "                echo $1",         // 203 bytes
	}

	opts := Options{
		Dialect:  "sh",
		Severity: "style",
		Norc:     true,
		Exclude:  []string{"1040"},
	}

	for i, s := range scripts {
		out, _, err := r.Run(ctx, s, opts)
		if err != nil {
			t.Fatalf("call %d (len=%d): Run() error: %v", i, len(s), err)
		}
		if len(out.Comments) == 0 {
			t.Fatalf("call %d: expected at least one comment (len=%d)", i, len(s))
		}
	}
}
