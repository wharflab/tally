package tally

import (
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestVisitStageAndAncestryRunScripts(t *testing.T) {
	t.Parallel()

	t.Run("single stage single RUN", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04
RUN echo hello
`)
		var visits []scriptVisit
		visitStageAndAncestryRunScripts(input, input.Facts, 0, func(v scriptVisit) {
			visits = append(visits, v)
		})
		if len(visits) != 1 {
			t.Fatalf("got %d visits, want 1", len(visits))
		}
		if visits[0].StageIndex != 0 || visits[0].Script != "echo hello" || visits[0].Run == nil {
			t.Errorf("visit = %+v", visits[0])
		}
	})

	t.Run("multiple RUN in source order", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04
RUN echo first
RUN echo second
`)
		var scripts []string
		visitStageAndAncestryRunScripts(input, input.Facts, 0, func(v scriptVisit) {
			scripts = append(scripts, v.Script)
		})
		want := []string{"echo first", "echo second"}
		if !slices.Equal(scripts, want) {
			t.Errorf("scripts = %v, want %v", scripts, want)
		}
	})

	t.Run("FROM ancestry walks parent then grandparent", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04 AS base
RUN echo grandparent

FROM base AS mid
RUN echo parent

FROM mid AS final
RUN echo final
`)
		var perStage []int
		visitStageAndAncestryRunScripts(input, input.Facts, 2, func(v scriptVisit) {
			perStage = append(perStage, v.StageIndex)
		})
		// Order: final stage (2), then parent (1), then grandparent (0).
		want := []int{2, 1, 0}
		if !slices.Equal(perStage, want) {
			t.Errorf("stage order = %v, want %v", perStage, want)
		}
	})

	t.Run("cycle guard via visited map", func(t *testing.T) {
		t.Parallel()
		// No Dockerfile syntax allows a true FROM cycle; the visited map is a
		// belt-and-suspenders defense. Confirm that calling on the last stage
		// of a linear chain terminates, as the cycle-guard logic exercises
		// the same code path.
		input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04 AS a
FROM a AS b
FROM b AS c
RUN echo ok
`)
		called := 0
		visitStageAndAncestryRunScripts(input, input.Facts, 2, func(v scriptVisit) {
			called++
			if called > 100 {
				t.Fatal("infinite loop")
			}
		})
	})

	t.Run("observable script file visited with extension variant", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04
COPY <<EOF /entrypoint.sh
#!/bin/sh
echo hello
EOF
`)
		var found bool
		var variant shell.Variant
		visitStageAndAncestryRunScripts(input, input.Facts, 0, func(v scriptVisit) {
			if v.Run == nil {
				found = true
				variant = v.Variant
			}
		})
		if !found {
			t.Fatal("observable script not visited")
		}
		if variant != shell.VariantBash {
			t.Errorf("variant = %v, want VariantBash (VariantFromScriptPath default)", variant)
		}
	})

	t.Run("semantic model nil returns after startStage", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", `FROM ubuntu:22.04
RUN echo hi
`)
		input.Semantic = nil
		called := 0
		visitStageAndAncestryRunScripts(input, input.Facts, 0, func(v scriptVisit) {
			called++
		})
		if called != 1 {
			t.Errorf("got %d visits, want 1", called)
		}
	})
}

func TestFindUserCreationCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant shell.Variant
		wantLen int
	}{
		{"bash useradd", "useradd alice", shell.VariantBash, 1},
		{"bash adduser", "adduser -D alice", shell.VariantBash, 1},
		{"bash non-creation", "echo nothing", shell.VariantBash, 0},
		{"cmd net user add", "net user alice pass /add", shell.VariantCmd, 1},
		{"cmd net user delete not matched", "net user alice /delete", shell.VariantCmd, 0},
		{"cmd net share /add not matched", "net share foo /add", shell.VariantCmd, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findUserCreationCmds(tt.script, tt.variant)
			if len(got) != tt.wantLen {
				t.Errorf("got %d cmds, want %d — %+v", len(got), tt.wantLen, got)
			}
		})
	}
}

func TestExtractCreatedUsername(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant shell.Variant
		want    string
	}{
		{"useradd simple", "useradd alice", shell.VariantBash, "alice"},
		{"useradd with flags", "useradd -r --home-dir /opt/app app", shell.VariantBash, "app"},
		{"adduser disabled-password", "adduser -D alice", shell.VariantBash, "alice"},
		{"net user /add", "net user alice pass /add", shell.VariantCmd, "alice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmds := findUserCreationCmds(tt.script, tt.variant)
			if len(cmds) != 1 {
				t.Fatalf("got %d cmds, want 1", len(cmds))
			}
			got := extractCreatedUsername(&cmds[0])
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindUserMembershipCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant shell.Variant
		want    []membershipInfo
	}{
		// POSIX useradd
		{
			name:    "useradd -G single",
			script:  "useradd -G docker app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},
		{
			name:    "useradd -G comma list",
			script:  "useradd -G docker,wheel app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker", "wheel"}}},
		},
		{
			name:    "useradd --groups=value",
			script:  "useradd --groups=docker app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},
		{
			name:    "useradd -g primary only no fire",
			script:  "useradd -g app app",
			variant: shell.VariantBash,
			want:    nil,
		},
		{
			name:    "useradd -g and -G",
			script:  "useradd -g app -G docker app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},

		// POSIX usermod
		{
			name:    "usermod -aG",
			script:  "usermod -aG docker app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},
		{
			name:    "usermod -a -G",
			script:  "usermod -a -G docker app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},
		{
			name:    "usermod -G without -a (replace)",
			script:  "usermod -G docker app",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},

		// POSIX gpasswd
		{
			name:    "gpasswd -a user group",
			script:  "gpasswd -a alice wheel",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "alice", Groups: []string{"wheel"}}},
		},
		{
			name:    "gpasswd --add user group",
			script:  "gpasswd --add alice wheel",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "alice", Groups: []string{"wheel"}}},
		},
		{
			name:    "gpasswd without -a no fire",
			script:  "gpasswd -d alice wheel",
			variant: shell.VariantBash,
			want:    nil,
		},

		// POSIX adduser / addgroup membership forms
		{
			name:    "adduser USER GROUP membership",
			script:  "adduser alice wheel",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "alice", Groups: []string{"wheel"}}},
		},
		{
			name:    "adduser -D alice creation no fire",
			script:  "adduser -D alice",
			variant: shell.VariantBash,
			want:    nil,
		},
		{
			name:    "addgroup USER GROUP membership",
			script:  "addgroup alice docker",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "alice", Groups: []string{"docker"}}},
		},
		{
			name:    "addgroup group-creation single positional no fire",
			script:  "addgroup docker",
			variant: shell.VariantBash,
			want:    nil,
		},
		{
			name:    "addgroup -S system group creation no fire",
			script:  "addgroup -S docker",
			variant: shell.VariantBash,
			want:    nil,
		},

		// Windows cmd
		{
			name:    "net localgroup /add",
			script:  "net localgroup Administrators app /add",
			variant: shell.VariantCmd,
			want:    []membershipInfo{{User: "app", Groups: []string{"Administrators"}}},
		},
		{
			name:    "net localgroup /delete not matched",
			script:  "net localgroup Administrators app /delete",
			variant: shell.VariantCmd,
			want:    nil,
		},

		// Windows PowerShell
		{
			name:    "Add-LocalGroupMember -Group -Member",
			script:  "Add-LocalGroupMember -Group docker -Member app",
			variant: shell.VariantPowerShell,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},
		{
			name:    "Add-LocalGroupMember positional",
			script:  "Add-LocalGroupMember docker app",
			variant: shell.VariantPowerShell,
			want:    []membershipInfo{{User: "app", Groups: []string{"docker"}}},
		},
		{
			name:    "Add-LocalGroupMember array member skipped",
			script:  `Add-LocalGroupMember -Group docker -Member @("u1","u2")`,
			variant: shell.VariantPowerShell,
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findUserMembershipCmds(tt.script, tt.variant)
			if !equalMembershipInfos(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFindUserMembershipCmdsEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant shell.Variant
		want    []membershipInfo
	}{
		{
			name:    "adduser with combined short -Dm no fire",
			script:  "adduser -Dm alice",
			variant: shell.VariantBash,
			want:    nil,
		},
		{
			name:    "gpasswd --add=USER positional GROUP",
			script:  "gpasswd --add=alice wheel",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "alice", Groups: []string{"wheel"}}},
		},
		{
			name:    "gpasswd -a=USER positional GROUP",
			script:  "gpasswd -a=alice wheel",
			variant: shell.VariantBash,
			want:    []membershipInfo{{User: "alice", Groups: []string{"wheel"}}},
		},
		{
			name:    "Add-LocalGroupMember with three positionals skipped",
			script:  "Add-LocalGroupMember docker app extra",
			variant: shell.VariantPowerShell,
			want:    nil,
		},
		{
			name:    "usermod no -G flag no fire",
			script:  "usermod -L app",
			variant: shell.VariantBash,
			want:    nil,
		},
		{
			name:    "usermod combined short with non-boolean flag skips",
			script:  "usermod -zG docker app",
			variant: shell.VariantBash,
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findUserMembershipCmds(tt.script, tt.variant)
			if !equalMembershipInfos(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func equalMembershipInfos(a, b []membershipInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].User != b[i].User {
			return false
		}
		if !slices.Equal(a[i].Groups, b[i].Groups) {
			return false
		}
	}
	return true
}
