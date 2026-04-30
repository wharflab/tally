package cmd

import (
	"testing"

	"github.com/docker/cli/cli-plugins/metadata"
)

func TestIsDockerLintPluginExecutable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "unix plugin", path: "/usr/local/lib/docker/cli-plugins/docker-lint", want: true},
		{name: "windows plugin", path: `C:\Users\me\.docker\cli-plugins\docker-lint.exe`, want: true},
		{name: "windows uppercase suffix", path: `C:\Users\me\.docker\cli-plugins\docker-lint.EXE`, want: true},
		{name: "standalone", path: "/usr/local/bin/tally", want: false},
		{name: "near miss", path: "/usr/local/bin/docker-tally", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsDockerLintPluginExecutable(tc.path); got != tc.want {
				t.Fatalf("IsDockerLintPluginExecutable(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestDockerPluginMetadata(t *testing.T) {
	t.Parallel()

	meta := dockerPluginMetadata()
	if meta.SchemaVersion != "0.1.0" {
		t.Fatalf("SchemaVersion = %q, want 0.1.0", meta.SchemaVersion)
	}
	if meta.Vendor != "Wharflab" {
		t.Fatalf("Vendor = %q, want Wharflab", meta.Vendor)
	}
	if meta.Version == "" {
		t.Fatal("Version should not be empty")
	}
	if meta.ShortDescription == "" {
		t.Fatal("ShortDescription should not be empty")
	}
	if meta.URL != "https://tally.wharflab.com/" {
		t.Fatalf("URL = %q, want https://tally.wharflab.com/", meta.URL)
	}
}

func TestDockerLintPluginCommandShape(t *testing.T) {
	t.Parallel()

	cmd := newDockerLintPluginCommand(nil)
	if cmd.Name() != "lint" {
		t.Fatalf("plugin command name = %q, want lint", cmd.Name())
	}
	if cmd.Version == "" {
		t.Fatal("plugin command should expose a version")
	}
	for _, unsupported := range []string{"lsp", "version"} {
		if sub, _, err := cmd.Find([]string{unsupported}); err == nil && sub != cmd {
			t.Fatalf("plugin command should not expose standalone subcommand %q", unsupported)
		}
	}
}

func TestStandaloneRootDoesNotExposeDockerMetadata(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	if sub, _, err := root.Find([]string{metadata.MetadataSubcommandName}); err == nil && sub != root {
		t.Fatalf("standalone root exposes %q", metadata.MetadataSubcommandName)
	}
}
