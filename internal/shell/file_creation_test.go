package shell

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestDetectFileCreation(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		variant    Variant
		wantNil    bool
		wantPath   string
		wantChmod  uint16
		wantUnsafe bool
	}{
		{
			name:     "simple echo to file",
			script:   `echo "hello world" > /app/config.txt`,
			variant:  VariantBash,
			wantPath: "/app/config.txt",
		},
		{
			name:      "echo with chmod",
			script:    `echo "#!/bin/bash" > /app/script.sh && chmod 755 /app/script.sh`,
			variant:   VariantBash,
			wantPath:  "/app/script.sh",
			wantChmod: 0o755,
		},
		{
			name:      "echo with 4-digit chmod",
			script:    `echo "data" > /app/file && chmod 0644 /app/file`,
			variant:   VariantBash,
			wantPath:  "/app/file",
			wantChmod: 0o644,
		},
		{
			name:    "relative path - skip",
			script:  `echo "data" > config.txt`,
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:       "shell variable - unsafe",
			script:     `echo "$HOME" > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true,
		},
		{
			name:    "complex script - skip",
			script:  `if [ -f /app/config ]; then echo "exists"; fi`,
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:    "stderr redirect (2>) - skip",
			script:  `echo "error" 2> /app/error.log`,
			variant: VariantBash,
			wantNil: true, // 2> is stderr, not file creation
		},
		{
			name:     "explicit stdout (1>) - detect",
			script:   `echo "data" 1> /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file", // 1> is explicit stdout, same as >
		},
		{
			name:      "symbolic chmod +x converts to 0755",
			script:    `echo "x" > /app/file && chmod +x /app/file`,
			variant:   VariantBash,
			wantPath:  "/app/file",
			wantChmod: 0o755, // +x on 0o644 base = 0755
		},
		{
			name:     "cat with redirect creates empty file",
			script:   `cat > /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file", // cat > file creates empty file
		},
		{
			name:     "cat heredoc to file",
			script:   "cat <<EOF > /app/config.txt\nhello world\nEOF",
			variant:  VariantBash,
			wantPath: "/app/config.txt",
		},
		{
			name:       "cat with file arg - unsafe",
			script:     `cat /etc/hosts > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // Content comes from file, not heredoc
		},
		{
			name:       "cat with flag - unsafe",
			script:     "cat -n <<EOF > /app/file\ndata\nEOF",
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // -n flag modifies content
		},
		{
			name:     "single quoted content",
			script:   `echo 'no $expansion' > /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file",
		},
		{
			name:    "command substitution - unsafe",
			script:  `echo "$(date)" > /app/file`,
			variant: VariantBash,
			wantPath: "/app/file",
			wantUnsafe: true,
		},
		{
			name:       "echo -n - unsafe (no trailing newline)",
			script:     `echo -n "data" > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // COPY heredoc always adds newline
		},
		{
			name:       "echo with unknown flag -x - unsafe",
			script:     `echo -x "data" > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // Unknown option letter
		},
		{
			name:     "echo with -- ends options",
			script:   `echo -- -n > /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file",
			// -n after -- is content, not a flag
		},
		{
			name:     "echo with bare dash is content",
			script:   `echo - > /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file",
			// bare "-" is content, not an option
		},
		{
			name:       "printf without newline - unsafe",
			script:     `printf "data" > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // printf doesn't add newline, COPY heredoc does
		},
		{
			name:    "non-file-creation command",
			script:  `apt-get update && apt-get install -y curl`,
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:    "non-POSIX shell",
			script:  `echo "data" > /app/file`,
			variant: VariantNonPOSIX,
			wantNil: true,
		},
		{
			name:     "chmod with multiple targets - no chmod extracted",
			script:   `echo "x" > /app/file && chmod 755 /app/file /app/other`,
			variant:  VariantBash,
			wantPath: "/app/file",
			// chmodMode should be empty since multiple targets are not supported
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectFileCreation(tt.script, tt.variant, nil)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.TargetPath != tt.wantPath {
				t.Errorf("TargetPath = %q, want %q", result.TargetPath, tt.wantPath)
			}

			if result.ChmodMode != tt.wantChmod {
				t.Errorf("ChmodMode = %q, want %q", result.ChmodMode, tt.wantChmod)
			}

			if result.HasUnsafeVariables != tt.wantUnsafe {
				t.Errorf("HasUnsafeVariables = %v, want %v", result.HasUnsafeVariables, tt.wantUnsafe)
			}
		})
	}
}

func TestIsPureFileCreation(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		variant Variant
		want    bool
	}{
		{
			name:    "echo to file",
			script:  `echo "hello" > /app/file`,
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "echo with chmod",
			script:  `echo "x" > /app/file && chmod 755 /app/file`,
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "apt-get command",
			script:  `apt-get update`,
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "mixed commands",
			script:  `apt-get update && echo "done" > /app/log`,
			variant: VariantBash,
			want:    false, // Mixed with non-file-creation commands - not pure
		},
		{
			name:    "no redirect",
			script:  `echo "hello world"`,
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "non-POSIX",
			script:  `echo "hello" > /app/file`,
			variant: VariantNonPOSIX,
			want:    false,
		},
		{
			name:    "apt-get chain - no file creation",
			script:  `apt-get update && apt-get install -y curl && apt-get clean`,
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "conda with echo to relative path",
			script:  `/opt/conda/bin/conda config && echo "test" > ~/.bashrc`,
			variant: VariantBash,
			want:    false, // Mixed commands AND relative path
		},
		{
			name:    "echo to home dir relative path",
			script:  `echo "test" > ~/.bashrc`,
			variant: VariantBash,
			want:    false, // ~ is not absolute path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPureFileCreation(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("IsPureFileCreation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOctalMode(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"755", true},
		{"0755", true},
		{"644", true},
		{"0644", true},
		{"777", true},
		{"0777", true},
		{"1755", true},  // sticky bit
		{"2755", true},  // setgid
		{"4755", true},  // setuid
		{"4777", true},  // setuid + all perms
		{"+x", false},
		{"u+rwx", false},
		{"a+r", false},
		{"", false},
		{"888", false},   // Invalid octal
		{"75", false},    // Too short
		{"07555", false}, // Too long (5 digits)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("isOctalMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsSymbolicMode(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"+x", true},
		{"+r", true},
		{"+w", true},
		{"+rwx", true},
		{"u+x", true},
		{"g+w", true},
		{"o+r", true},
		{"a+x", true},
		{"ug+rx", true},
		{"-x", true},
		{"=rwx", true},
		{"755", false},
		{"0755", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isSymbolicMode(tt.input)
			if got != tt.want {
				t.Errorf("isSymbolicMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSymbolicToOctal(t *testing.T) {
	// Base mode 0o644 (default for newly created files)
	tests := []struct {
		symbolic string
		base     int
		want     uint16
	}{
		// Add execute
		{"+x", 0o644, 0o755},
		{"a+x", 0o644, 0o755},
		{"u+x", 0o644, 0o744},
		{"g+x", 0o644, 0o654},
		{"o+x", 0o644, 0o645},
		{"ug+x", 0o644, 0o754},

		// Add read (already has it for user)
		{"+r", 0o644, 0o644},
		{"o+r", 0o644, 0o644}, // other already has read

		// Add write
		{"+w", 0o644, 0o666},
		{"g+w", 0o644, 0o664},
		{"o+w", 0o644, 0o646},

		// Combined permissions
		{"+rwx", 0o644, 0o777},
		{"u+rwx", 0o644, 0o744}, // user already has rw, adds x
		{"go+rx", 0o644, 0o655},

		// Remove permissions
		{"-x", 0o644, 0o644},  // no execute to remove
		{"-w", 0o644, 0o444},  // removes write from user
		{"o-r", 0o644, 0o640}, // removes read from other

		// Set exactly
		{"=rwx", 0o644, 0o777},  // sets all to rwx
		{"u=rwx", 0o644, 0o744}, // sets user to rwx exactly
		{"go=rx", 0o644, 0o655}, // sets group/other to rx exactly

		// Unsupported modes (return 0)
		{"+X", 0o644, 0},
		{"+s", 0o644, 0},
		{"+t", 0o644, 0},
		{"", 0o644, 0},
	}

	for _, tt := range tests {
		t.Run(tt.symbolic, func(t *testing.T) {
			got := symbolicToOctal(tt.symbolic, tt.base)
			if got != tt.want {
				t.Errorf("symbolicToOctal(%q, %04o) = %04o, want %04o", tt.symbolic, tt.base, got, tt.want)
			}
		})
	}
}

func TestParseOctalMode(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"755", 0o755},
		{"0755", 0o755},
		{"644", 0o644},
		{"0644", 0o644},
		{"4755", 0o4755}, // setuid
		{"2755", 0o2755}, // setgid
		{"1755", 0o1755}, // sticky
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("ParseOctalMode(%q) = %04o, want %04o", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatOctalMode(t *testing.T) {
	tests := []struct {
		input uint16
		want  string
	}{
		{0o755, "0755"},
		{0o644, "0644"},
		{0o4755, "4755"}, // setuid
		{0o2755, "2755"}, // setgid
		{0o1755, "1755"}, // sticky
		{0, ""},          // no mode
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("FormatOctalMode(%04o) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectFileCreationWithKnownVars(t *testing.T) {
	knownVars := func(name string) bool {
		return name == "APP_CONFIG" || name == "VERSION"
	}

	tests := []struct {
		name       string
		script     string
		wantUnsafe bool
	}{
		{
			name:       "known ARG variable",
			script:     `echo "$APP_CONFIG" > /app/config`,
			wantUnsafe: false,
		},
		{
			name:       "unknown variable",
			script:     `echo "$HOME" > /app/config`,
			wantUnsafe: true,
		},
		{
			name:       "mixed known and unknown",
			script:     `echo "$APP_CONFIG $HOME" > /app/config`,
			wantUnsafe: true,
		},
		{
			name:       "literal content",
			script:     `echo "hello world" > /app/config`,
			wantUnsafe: false,
		},
		{
			name:       "complex expansion - length",
			script:     `echo "${#APP_CONFIG}" > /app/config`,
			wantUnsafe: true, // ${#VAR} is complex expansion
		},
		{
			name:       "complex expansion - default",
			script:     `echo "${APP_CONFIG:-default}" > /app/config`,
			wantUnsafe: true, // ${VAR:-default} is complex expansion
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectFileCreation(tt.script, VariantBash, knownVars)
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.HasUnsafeVariables != tt.wantUnsafe {
				t.Errorf("HasUnsafeVariables = %v, want %v", result.HasUnsafeVariables, tt.wantUnsafe)
			}
		})
	}
}

func TestDetectStandaloneChmod(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		variant  Variant
		wantNil  bool
		wantMode uint16
		wantPath string
	}{
		{
			name:     "simple chmod 755",
			script:   `chmod 755 /app/script.sh`,
			variant:  VariantBash,
			wantMode: 0o755,
			wantPath: "/app/script.sh",
		},
		{
			name:     "chmod 0644",
			script:   `chmod 0644 /app/config`,
			variant:  VariantBash,
			wantMode: 0o644,
			wantPath: "/app/config",
		},
		{
			name:     "chmod +x (converted to octal)",
			script:   `chmod +x /app/run.sh`,
			variant:  VariantBash,
			wantMode: 0o755, // +x on base 0644 = 0755
			wantPath: "/app/run.sh",
		},
		{
			name:    "not chmod command",
			script:  `echo "test"`,
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:    "chmod with chained command",
			script:  `chmod 755 /app/file && echo done`,
			variant: VariantBash,
			wantNil: true, // Not standalone
		},
		{
			name:    "chmod with multiple targets",
			script:  `chmod 755 /app/file1 /app/file2`,
			variant: VariantBash,
			wantNil: true, // Multiple targets not supported
		},
		{
			name:    "non-POSIX shell",
			script:  `chmod 755 /app/file`,
			variant: VariantNonPOSIX,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectStandaloneChmod(tt.script, tt.variant)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Mode != tt.wantMode {
				t.Errorf("Mode = %04o, want %04o", result.Mode, tt.wantMode)
			}
			if result.Target != tt.wantPath {
				t.Errorf("Target = %q, want %q", result.Target, tt.wantPath)
			}
		})
	}
}

func TestDetectFileCreationCatHeredoc(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		wantNil     bool
		wantPath    string
		wantContent string
		wantUnsafe  bool
	}{
		{
			name:        "cat heredoc simple",
			script:      "cat <<EOF > /app/config\nhello world\nEOF",
			wantPath:    "/app/config",
			wantContent: "hello world\n",
		},
		{
			name:        "cat heredoc with dash (tab stripping)",
			script:      "cat <<-EOF > /app/config\n\thello\n\tworld\nEOF",
			wantPath:    "/app/config",
			wantContent: "hello\nworld\n",
		},
		{
			name:       "cat heredoc with variable - unsafe",
			script:     "cat <<EOF > /app/config\nhello $USER\nEOF",
			wantPath:   "/app/config",
			wantUnsafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectFileCreation(tt.script, VariantBash, nil)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.TargetPath != tt.wantPath {
				t.Errorf("TargetPath = %q, want %q", result.TargetPath, tt.wantPath)
			}
			if tt.wantContent != "" && result.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tt.wantContent)
			}
			if result.HasUnsafeVariables != tt.wantUnsafe {
				t.Errorf("HasUnsafeVariables = %v, want %v", result.HasUnsafeVariables, tt.wantUnsafe)
			}
		})
	}
}

func TestDetectFileCreationEchoEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		wantNil     bool
		wantPath    string
		wantContent string
		wantUnsafe  bool
	}{
		{
			name:        "echo with no arguments",
			script:      `echo > /app/file`,
			wantPath:    "/app/file",
			wantContent: "\n",
		},
		{
			name:       "echo -e with escape",
			script:     `echo -e "hello\tworld" > /app/file`,
			wantPath:   "/app/file",
			wantUnsafe: true,
		},
		{
			name:        "echo with single quotes",
			script:      `echo 'literal $VAR' > /app/file`,
			wantPath:    "/app/file",
			wantContent: "literal $VAR\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectFileCreation(tt.script, VariantBash, nil)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.TargetPath != tt.wantPath {
				t.Errorf("TargetPath = %q, want %q", result.TargetPath, tt.wantPath)
			}
			if tt.wantContent != "" && result.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tt.wantContent)
			}
			if result.HasUnsafeVariables != tt.wantUnsafe {
				t.Errorf("HasUnsafeVariables = %v, want %v", result.HasUnsafeVariables, tt.wantUnsafe)
			}
		})
	}
}

func TestDetectFileCreationWithUmask(t *testing.T) {
	tests := []struct {
		name      string
		script    string
		wantNil   bool
		wantPath  string
		wantChmod uint16
	}{
		{
			name:      "umask 077 before file creation",
			script:    `umask 077 && echo "secret" > /app/config`,
			wantPath:  "/app/config",
			wantChmod: 0o600, // effective mode from umask
		},
		{
			name:      "umask 0077 (4-digit) before file creation",
			script:    `umask 0077 && echo "secret" > /app/config`,
			wantPath:  "/app/config",
			wantChmod: 0o600,
		},
		{
			name:      "umask 027 before file creation",
			script:    `umask 027 && echo "data" > /app/file`,
			wantPath:  "/app/file",
			wantChmod: 0o640, // effective mode from umask
		},
		{
			name:      "umask 022 (default) - no chmod needed",
			script:    `umask 022 && echo "data" > /app/file`,
			wantPath:  "/app/file",
			wantChmod: 0, // default umask, no chmod needed
		},
		{
			name:      "umask 000 - all permissions",
			script:    `umask 000 && echo "data" > /app/file`,
			wantPath:  "/app/file",
			wantChmod: 0o666, // no masking, full permissions
		},
		{
			name:      "explicit chmod overrides umask",
			script:    `umask 077 && echo "x" > /app/file && chmod 755 /app/file`,
			wantPath:  "/app/file",
			wantChmod: 0o755, // explicit chmod takes precedence
		},
		{
			name:     "no umask - no chmod",
			script:   `echo "data" > /app/file`,
			wantPath: "/app/file",
			// No umask, no chmod - default permissions apply
		},
		{
			name:      "umask with other commands before file creation",
			script:    `umask 077 && mkdir -p /app && echo "secret" > /app/config`,
			wantPath:  "/app/config",
			wantChmod: 0o600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectFileCreation(tt.script, VariantBash, nil)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.TargetPath != tt.wantPath {
				t.Errorf("TargetPath = %q, want %q", result.TargetPath, tt.wantPath)
			}
			if result.ChmodMode != tt.wantChmod {
				t.Errorf("ChmodMode = %04o, want %04o", result.ChmodMode, tt.wantChmod)
			}
		})
	}
}

func TestParseUmask(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   uint16
		wantOk bool
	}{
		{"simple 077", []string{"umask", "077"}, 0o077, true},
		{"4-digit 0077", []string{"umask", "0077"}, 0o077, true},
		{"022", []string{"umask", "022"}, 0o022, true},
		{"000", []string{"umask", "000"}, 0o000, true},
		{"no args (print)", []string{"umask"}, 0, false},
		{"symbolic mode", []string{"umask", "u=rwx,go="}, 0, false}, // Not supported
		{"too many args", []string{"umask", "077", "extra"}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build a minimal CallExpr for testing
			script := strings.Join(tt.args, " ")
			prog, err := parseScript(script, VariantBash)
			if err != nil {
				t.Fatalf("parseScript failed: %v", err)
			}
			if len(prog.Stmts) == 0 {
				t.Fatal("no statements parsed")
			}
			call, ok := prog.Stmts[0].Cmd.(*syntax.CallExpr)
			if !ok {
				t.Fatal("expected CallExpr")
			}
			got, gotOk := parseUmask(call)
			if gotOk != tt.wantOk {
				t.Errorf("parseUmask() ok = %v, want %v", gotOk, tt.wantOk)
			}
			if gotOk && got != tt.want {
				t.Errorf("parseUmask() = %04o, want %04o", got, tt.want)
			}
		})
	}
}
