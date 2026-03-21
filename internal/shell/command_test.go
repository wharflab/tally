package shell

import (
	"slices"
	"testing"
)

func TestFindCommands_AllCommandsIncludesWrappedCommands(t *testing.T) {
	t.Parallel()

	got := FindCommands(
		"env PIP_INDEX_URL=https://example.com/simple pip install flask && npm install express",
		VariantBash,
	)

	names := make([]string, 0, len(got))
	for _, cmd := range got {
		names = append(names, cmd.Name)
	}

	want := []string{"env", "pip", "npm"}
	if !slices.Equal(names, want) {
		t.Fatalf("FindCommands(all) names = %v, want %v", names, want)
	}
}

func TestCommandInfo_HasFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  CommandInfo
		flag string
		want bool
	}{
		{
			name: "short flag -y",
			cmd:  CommandInfo{Args: []string{"-y", "install", "curl"}},
			flag: "-y",
			want: true,
		},
		{
			name: "short flag y without dash",
			cmd:  CommandInfo{Args: []string{"-y", "install", "curl"}},
			flag: "y",
			want: true,
		},
		{
			name: "combined short flags -yq contains -y",
			cmd:  CommandInfo{Args: []string{"-yq", "install", "curl"}},
			flag: "-y",
			want: true,
		},
		{
			name: "combined short flags -yq contains -q",
			cmd:  CommandInfo{Args: []string{"-yq", "install", "curl"}},
			flag: "-q",
			want: true,
		},
		{
			name: "long flag --yes",
			cmd:  CommandInfo{Args: []string{"--yes", "install", "curl"}},
			flag: "--yes",
			want: true,
		},
		{
			name: "long flag --assume-yes",
			cmd:  CommandInfo{Args: []string{"--assume-yes", "install", "curl"}},
			flag: "--assume-yes",
			want: true,
		},
		{
			name: "flag not present",
			cmd:  CommandInfo{Args: []string{"install", "curl"}},
			flag: "-y",
			want: false,
		},
		{
			name: "flag with value --quiet=2",
			cmd:  CommandInfo{Args: []string{"--quiet=2", "install"}},
			flag: "--quiet",
			want: true,
		},
		{
			name: "short flag in middle -qq",
			cmd:  CommandInfo{Args: []string{"-qq", "install"}},
			flag: "-q",
			want: true,
		},
		{
			name: "long flag not matching short",
			cmd:  CommandInfo{Args: []string{"--yes", "install"}},
			flag: "-y",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cmd.HasFlag(tt.flag); got != tt.want {
				t.Errorf("CommandInfo.HasFlag(%q) = %v, want %v", tt.flag, got, tt.want)
			}
		})
	}
}

func TestCommandInfo_HasAnyFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		cmd   CommandInfo
		flags []string
		want  bool
	}{
		{
			name:  "has first flag",
			cmd:   CommandInfo{Args: []string{"-y", "install"}},
			flags: []string{"-y", "--yes"},
			want:  true,
		},
		{
			name:  "has second flag",
			cmd:   CommandInfo{Args: []string{"--yes", "install"}},
			flags: []string{"-y", "--yes"},
			want:  true,
		},
		{
			name:  "has none",
			cmd:   CommandInfo{Args: []string{"install", "curl"}},
			flags: []string{"-y", "--yes"},
			want:  false,
		},
		{
			name:  "apt-get -qq is covered by CountFlag not HasFlag",
			cmd:   CommandInfo{Args: []string{"-qq", "install"}},
			flags: []string{"-y", "--yes"}, // -qq requires CountFlag check, not HasAnyFlag
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cmd.HasAnyFlag(tt.flags...); got != tt.want {
				t.Errorf("CommandInfo.HasAnyFlag(%v) = %v, want %v", tt.flags, got, tt.want)
			}
		})
	}
}

func TestCommandInfo_CountFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  CommandInfo
		flag string
		want int
	}{
		{
			name: "single -q",
			cmd:  CommandInfo{Args: []string{"-q", "install"}},
			flag: "-q",
			want: 1,
		},
		{
			name: "double -q -q",
			cmd:  CommandInfo{Args: []string{"-q", "-q", "install"}},
			flag: "-q",
			want: 2,
		},
		{
			name: "combined -qq",
			cmd:  CommandInfo{Args: []string{"-qq", "install"}},
			flag: "-q",
			want: 2,
		},
		{
			name: "double --quiet --quiet",
			cmd:  CommandInfo{Args: []string{"--quiet", "--quiet", "install"}},
			flag: "--quiet",
			want: 2,
		},
		{
			name: "no flags",
			cmd:  CommandInfo{Args: []string{"install"}},
			flag: "-q",
			want: 0,
		},
		{
			name: "mixed -q and --quiet",
			cmd:  CommandInfo{Args: []string{"-q", "--quiet", "install"}},
			flag: "-q",
			want: 1, // Only counts -q, not --quiet
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cmd.CountFlag(tt.flag); got != tt.want {
				t.Errorf("CommandInfo.CountFlag(%q) = %d, want %d", tt.flag, got, tt.want)
			}
		})
	}
}

func TestCommandInfo_GetArgValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  CommandInfo
		flag string
		want string
	}{
		{
			name: "-q=2",
			cmd:  CommandInfo{Args: []string{"-q=2", "install"}},
			flag: "-q",
			want: "2",
		},
		{
			name: "--quiet=2",
			cmd:  CommandInfo{Args: []string{"--quiet=2", "install"}},
			flag: "--quiet",
			want: "2",
		},
		{
			name: "flag without explicit value",
			cmd:  CommandInfo{Args: []string{"-q", "install"}},
			flag: "-q",
			want: "install", // GetArgValue returns next arg; caller decides if valid
		},
		{
			name: "flag not present",
			cmd:  CommandInfo{Args: []string{"install"}},
			flag: "-q",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cmd.GetArgValue(tt.flag); got != tt.want {
				t.Errorf("CommandInfo.GetArgValue(%q) = %q, want %q", tt.flag, got, tt.want)
			}
		})
	}
}

func TestCommandInfo_PowerShellFlags(t *testing.T) {
	t.Parallel()

	cmd := CommandInfo{
		Variant: VariantPowerShell,
		Name:    "invoke-webrequest",
		Args:    []string{"-Uri", "https://example.com/app.tar.gz", "-OutFile:C:\\tmp\\app.tar.gz", "-Verbose:$VerbosePreference"},
	}

	if !cmd.HasFlag("-uri") {
		t.Fatal("expected -Uri flag to be present")
	}
	if !cmd.HasFlag("-outfile") {
		t.Fatal("expected -OutFile flag to be present")
	}
	if got := cmd.GetArgValue("-Uri"); got != "https://example.com/app.tar.gz" {
		t.Fatalf("GetArgValue(-Uri) = %q, want %q", got, "https://example.com/app.tar.gz")
	}
	if got := cmd.GetArgValue("-OutFile"); got != `C:\tmp\app.tar.gz` {
		t.Fatalf("GetArgValue(-OutFile) = %q, want %q", got, `C:\tmp\app.tar.gz`)
	}
	if got := cmd.CountFlag("-Verbose"); got != 1 {
		t.Fatalf("CountFlag(-Verbose) = %d, want 1", got)
	}
}

func TestCommandInfo_PowerShellNativeToolFlagsUseNativeParsing(t *testing.T) {
	t.Parallel()

	curlCmd := CommandInfo{
		Variant: VariantPowerShell,
		Name:    "curl",
		Args:    []string{"-fsSL", "-o", "app.tar.gz", "https://example.com/app.tar.gz"},
	}
	if !curlCmd.HasFlag("-f") {
		t.Fatal("expected curl -f flag to be present")
	}
	if !curlCmd.HasFlag("-s") {
		t.Fatal("expected curl -s flag to be present")
	}
	if got := curlCmd.GetArgValue("-o"); got != "app.tar.gz" {
		t.Fatalf("GetArgValue(-o) = %q, want %q", got, "app.tar.gz")
	}

	tarCmd := CommandInfo{
		Variant: VariantPowerShell,
		Name:    "tar",
		Args:    []string{"--extract", "-f", "app.tar.gz", "-C", "/tools"},
	}
	if !tarCmd.HasFlag("--extract") {
		t.Fatal("expected tar --extract flag to be present")
	}
	if got := tarCmd.GetArgValue("-C"); got != "/tools" {
		t.Fatalf("GetArgValue(-C) = %q, want %q", got, "/tools")
	}
}

func TestFindCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		script     string
		cmdNames   []string
		wantCount  int
		wantSubcmd string
	}{
		{
			name:       "apt-get install",
			script:     "apt-get install -y curl",
			cmdNames:   []string{"apt-get"},
			wantCount:  1,
			wantSubcmd: "install",
		},
		{
			name:       "multiple apt-get commands",
			script:     "apt-get update && apt-get install -y curl",
			cmdNames:   []string{"apt-get"},
			wantCount:  2,
			wantSubcmd: "update",
		},
		{
			name:       "yum install",
			script:     "yum install httpd",
			cmdNames:   []string{"yum"},
			wantCount:  1,
			wantSubcmd: "install",
		},
		{
			name:       "dnf and microdnf",
			script:     "dnf install -y httpd && microdnf install nginx",
			cmdNames:   []string{"dnf", "microdnf"},
			wantCount:  2,
			wantSubcmd: "install",
		},
		{
			name:       "zypper install",
			script:     "zypper install -n httpd",
			cmdNames:   []string{"zypper"},
			wantCount:  1,
			wantSubcmd: "install",
		},
		{
			name:       "with env wrapper",
			script:     "env DEBIAN_FRONTEND=noninteractive apt-get install curl",
			cmdNames:   []string{"apt-get"},
			wantCount:  1,
			wantSubcmd: "install",
		},
		{
			name:       "with sh -c wrapper",
			script:     "sh -c 'apt-get install curl'",
			cmdNames:   []string{"apt-get"},
			wantCount:  1,
			wantSubcmd: "install",
		},
		{
			name:       "no matching command",
			script:     "echo hello",
			cmdNames:   []string{"apt-get"},
			wantCount:  0,
			wantSubcmd: "",
		},
		{
			name:       "command with path",
			script:     "/usr/bin/apt-get install curl",
			cmdNames:   []string{"apt-get"},
			wantCount:  1,
			wantSubcmd: "install",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmds := FindCommands(tt.script, VariantBash, tt.cmdNames...)
			if len(cmds) != tt.wantCount {
				t.Errorf("FindCommands() returned %d commands, want %d", len(cmds), tt.wantCount)
			}
			if tt.wantCount > 0 && len(cmds) > 0 && cmds[0].Subcommand != tt.wantSubcmd {
				t.Errorf("first command subcommand = %q, want %q", cmds[0].Subcommand, tt.wantSubcmd)
			}
		})
	}
}

func TestFindCommands_PowerShell(t *testing.T) {
	t.Parallel()

	script := `Invoke-WebRequest https://example.com/app.tar.gz -OutFile C:\tmp\app.tar.gz; tar.exe -xf C:\tmp\app.tar.gz -C C:\tools`
	cmds := FindCommands(script, VariantPowerShell, "invoke-webrequest", "tar")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	if cmds[0].Name != "invoke-webrequest" {
		t.Fatalf("first command name = %q, want %q", cmds[0].Name, "invoke-webrequest")
	}
	if cmds[0].Subcommand != "https://example.com/app.tar.gz" {
		t.Fatalf("first subcommand = %q, want url", cmds[0].Subcommand)
	}
	if got := cmds[0].GetArgValue("-OutFile"); got != `C:\tmp\app.tar.gz` {
		t.Fatalf("GetArgValue(-OutFile) = %q, want %q", got, `C:\tmp\app.tar.gz`)
	}

	if cmds[1].Name != "tar" {
		t.Fatalf("second command name = %q, want %q", cmds[1].Name, "tar")
	}
	if !IsTarExtract(&cmds[1]) {
		t.Fatal("expected tar command to be detected as extract")
	}
}

func TestFindCommands_Cmd(t *testing.T) {
	t.Parallel()

	script := `choco install git python && setx PATH "%PATH%;C:\Tools"`
	cmds := FindCommands(script, VariantCmd, "choco", "setx")
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}

	if cmds[0].Name != "choco" || cmds[0].Subcommand != "install" {
		t.Fatalf("first command = %#v, want choco install", cmds[0])
	}
	if cmds[1].Name != "setx" || cmds[1].Subcommand != "PATH" {
		t.Fatalf("second command = %#v, want setx PATH", cmds[1])
	}
}

func TestCommandNamesWithVariant_Cmd(t *testing.T) {
	t.Parallel()

	script := `cmd /c "echo hi" || py -m pip install -U pip`
	got := CommandNamesWithVariant(script, VariantCmd)
	want := []string{"cmd", "py"}
	if !slices.Equal(got, want) {
		t.Fatalf("CommandNamesWithVariant() = %v, want %v", got, want)
	}
}

func TestAnalyzeCmdScript(t *testing.T) {
	t.Parallel()

	script := `echo hi && echo bye`
	analysis := AnalyzeCmdScript(script)
	if analysis == nil {
		t.Fatal("expected analysis")
	}
	if !analysis.HasConditionals {
		t.Fatal("expected conditional execution to be detected")
	}

	script = `setlocal enabledelayedexpansion`
	analysis = AnalyzeCmdScript(script)
	if analysis == nil {
		t.Fatal("expected analysis for setlocal script")
	}
	if !analysis.HasControlFlow {
		t.Fatal("expected control flow to be detected")
	}

	script = `setx PATH '%PATH%;C:\Tools'`
	analysis = AnalyzeCmdScript(script)
	if analysis == nil {
		t.Fatal("expected analysis for setx script")
	}
	if !analysis.HasVariableReferences {
		t.Fatal("expected PATH expansion to be detected")
	}
	if analysis.HasBatchOnlySyntax() != true {
		t.Fatal("expected PATH expansion to be treated as batch-only syntax")
	}
}

func TestCommandNamesWithVariant_PowerShell(t *testing.T) {
	t.Parallel()

	script := `iwr -Uri https://example.com/app.tar.xz -OutFile C:\tmp\app.tar.xz; tar.exe --extract -f C:\tmp\app.tar.xz -C C:\tools`
	got := CommandNamesWithVariant(script, VariantPowerShell)
	want := []string{"iwr", "tar"}
	if !slices.Equal(got, want) {
		t.Fatalf("CommandNamesWithVariant() = %v, want %v", got, want)
	}
}

func TestFindCommands_WithFlags(t *testing.T) {
	t.Parallel()
	script := "apt-get install -y -q curl"
	cmds := FindCommands(script, VariantBash, "apt-get")

	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}

	cmd := cmds[0]
	if cmd.Subcommand != "install" {
		t.Errorf("subcommand = %q, want %q", cmd.Subcommand, "install")
	}
	if !cmd.HasFlag("-y") {
		t.Error("expected -y flag to be present")
	}
	if !cmd.HasFlag("-q") {
		t.Error("expected -q flag to be present")
	}
	if cmd.HasFlag("--verbose") {
		t.Error("expected --verbose flag to be absent")
	}
}

func TestFindCommands_SubcommandPosition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		script             string
		cmdName            string
		wantSubcmd         string
		wantSubcmdLine     int
		wantSubcmdStartCol int
		wantSubcmdEndCol   int
	}{
		{
			name:               "apt-get install",
			script:             "apt-get install curl",
			cmdName:            "apt-get",
			wantSubcmd:         "install",
			wantSubcmdLine:     0,
			wantSubcmdStartCol: 8,
			wantSubcmdEndCol:   15,
		},
		{
			name:               "apt-get with flags before subcommand",
			script:             "apt-get -q install curl",
			cmdName:            "apt-get",
			wantSubcmd:         "install",
			wantSubcmdLine:     0,
			wantSubcmdStartCol: 11,
			wantSubcmdEndCol:   18,
		},
		{
			name:               "yum install",
			script:             "yum install httpd",
			cmdName:            "yum",
			wantSubcmd:         "install",
			wantSubcmdLine:     0,
			wantSubcmdStartCol: 4,
			wantSubcmdEndCol:   11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmds := FindCommands(tt.script, VariantBash, tt.cmdName)
			if len(cmds) != 1 {
				t.Fatalf("expected 1 command, got %d", len(cmds))
			}
			cmd := cmds[0]

			if cmd.Subcommand != tt.wantSubcmd {
				t.Errorf("Subcommand = %q, want %q", cmd.Subcommand, tt.wantSubcmd)
			}
			if cmd.SubcommandLine != tt.wantSubcmdLine {
				t.Errorf("SubcommandLine = %d, want %d", cmd.SubcommandLine, tt.wantSubcmdLine)
			}
			if cmd.SubcommandStartCol != tt.wantSubcmdStartCol {
				t.Errorf("SubcommandStartCol = %d, want %d", cmd.SubcommandStartCol, tt.wantSubcmdStartCol)
			}
			if cmd.SubcommandEndCol != tt.wantSubcmdEndCol {
				t.Errorf("SubcommandEndCol = %d, want %d", cmd.SubcommandEndCol, tt.wantSubcmdEndCol)
			}
		})
	}
}

func TestAptGetYesDetection(t *testing.T) {
	t.Parallel()
	// Test all the ways to specify "yes" for apt-get per DL3014
	tests := []struct {
		name    string
		script  string
		wantYes bool
	}{
		{
			name:    "no yes flag",
			script:  "apt-get install python",
			wantYes: false,
		},
		{
			name:    "-y flag",
			script:  "apt-get install -y python",
			wantYes: true,
		},
		{
			name:    "-y before subcommand",
			script:  "apt-get -y install python",
			wantYes: true,
		},
		{
			name:    "--yes flag",
			script:  "apt-get --yes install python",
			wantYes: true,
		},
		{
			name:    "--assume-yes flag",
			script:  "apt-get --assume-yes install python",
			wantYes: true,
		},
		{
			name:    "-qq flag",
			script:  "apt-get install -qq python",
			wantYes: true,
		},
		{
			name:    "-q -q separate",
			script:  "apt-get install -q -q python",
			wantYes: true,
		},
		{
			name:    "-q=2",
			script:  "apt-get install -q=2 python",
			wantYes: true,
		},
		{
			name:    "--quiet --quiet",
			script:  "apt-get install --quiet --quiet python",
			wantYes: true,
		},
		{
			name:    "single -q not enough",
			script:  "apt-get install -q python",
			wantYes: false,
		},
		{
			name:    "single --quiet not enough",
			script:  "apt-get install --quiet python",
			wantYes: false,
		},
		{
			name:    "combined -yq",
			script:  "apt-get install -yq python",
			wantYes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmds := FindCommands(tt.script, VariantBash, "apt-get")
			if len(cmds) != 1 {
				t.Fatalf("expected 1 command, got %d", len(cmds))
			}
			cmd := cmds[0]

			// Check the various yes conditions per DL3014
			hasYes := cmd.HasAnyFlag("-y", "--yes", "-qq", "--assume-yes") ||
				cmd.CountFlag("-q") >= 2 ||
				cmd.CountFlag("--quiet") >= 2 ||
				cmd.GetArgValue("-q") == "2"

			if hasYes != tt.wantYes {
				t.Errorf("hasYes = %v, want %v", hasYes, tt.wantYes)
			}
		})
	}
}
