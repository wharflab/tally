package shell

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestDetectFileCreation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		script     string
		variant    Variant
		wantNil    bool
		wantPath   string
		wantChmod  uint16
		wantAppend bool
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
			name:       "command substitution - unsafe",
			script:     `echo "$(date)" > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
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
			name:     "printf with escape sequences ending in newline",
			script:   `printf 'line1\nline2\n' > /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file",
		},
		{
			name:       "printf with escape sequences not ending in newline",
			script:     `printf 'line1\nline2' > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // no trailing newline after processing
		},
		{
			name:       "printf with unsupported escape sequence",
			script:     `printf 'data\x41\n' > /app/file`,
			variant:    VariantBash,
			wantPath:   "/app/file",
			wantUnsafe: true, // \x is unsupported
		},
		{
			name:     "printf with percent-s and newline escape",
			script:   `printf '%s\n' 'hello' > /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file",
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
			variant: VariantPowerShell,
			wantNil: true,
		},
		{
			name:     "chmod with multiple targets - no chmod extracted",
			script:   `echo "x" > /app/file && chmod 755 /app/file /app/other`,
			variant:  VariantBash,
			wantPath: "/app/file",
			// chmodMode should be empty since multiple targets are not supported
		},
		{
			name:     "tee with heredoc to file",
			script:   "<<EOF tee /app/config.txt\nhello world\nEOF",
			variant:  VariantBash,
			wantPath: "/app/config.txt",
		},
		{
			name:       "tee -a with heredoc (append)",
			script:     "<<EOF tee -a /app/config.txt\nextra line\nEOF",
			variant:    VariantBash,
			wantPath:   "/app/config.txt",
			wantAppend: true,
		},
		{
			name:    "tee with multiple files - skip",
			script:  "<<EOF tee /app/file1 /app/file2\ndata\nEOF",
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:    "tee with relative path - skip",
			script:  "<<EOF tee config.txt\ndata\nEOF",
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:    "tee without file arg - skip",
			script:  "<<EOF tee\ndata\nEOF",
			variant: VariantBash,
			wantNil: true,
		},
		{
			name:     "tee with stdout suppressed",
			script:   "<<EOF tee /app/config.txt > /dev/null\nhello\nEOF",
			variant:  VariantBash,
			wantPath: "/app/config.txt",
		},
		{
			name:    "tee with redirect to another file - skip",
			script:  "<<EOF tee /app/config.txt > /tmp/log\ndata\nEOF",
			variant: VariantBash,
			wantNil: true, // redirect creates second file, can't convert to single COPY
		},
		{
			name:    "tee with stderr redirect - skip",
			script:  "<<EOF tee /app/config.txt 2> /tmp/err\ndata\nEOF",
			variant: VariantBash,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
				t.Errorf("ChmodMode = %04o, want %04o", result.ChmodMode, tt.wantChmod)
			}

			if result.IsAppend != tt.wantAppend {
				t.Errorf("IsAppend = %v, want %v", result.IsAppend, tt.wantAppend)
			}

			if result.HasUnsafeVariables != tt.wantUnsafe {
				t.Errorf("HasUnsafeVariables = %v, want %v", result.HasUnsafeVariables, tt.wantUnsafe)
			}
		})
	}
}

func TestIsPureFileCreation(t *testing.T) {
	t.Parallel()
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
			variant: VariantPowerShell,
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
			t.Parallel()
			got := IsPureFileCreation(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("IsPureFileCreation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOctalMode(t *testing.T) {
	t.Parallel()
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
		{"1755", true}, // sticky bit
		{"2755", true}, // setgid
		{"4755", true}, // setuid
		{"4777", true}, // setuid + all perms
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
			t.Parallel()
			got := IsOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("IsOctalMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsSymbolicMode(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := IsSymbolicMode(tt.input)
			if got != tt.want {
				t.Errorf("IsSymbolicMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplySymbolicMode(t *testing.T) {
	t.Parallel()
	// Base mode 0o644 (default for newly created files)
	tests := []struct {
		symbolic string
		base     uint16
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
			t.Parallel()
			got := ApplySymbolicMode(tt.symbolic, tt.base)
			if got != tt.want {
				t.Errorf("ApplySymbolicMode(%q, %04o) = %04o, want %04o", tt.symbolic, tt.base, got, tt.want)
			}
		})
	}
}

func TestParseOctalMode(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := ParseOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("ParseOctalMode(%q) = %04o, want %04o", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatOctalMode(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := FormatOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("FormatOctalMode(%04o) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectFileCreationWithKnownVars(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
			variant: VariantPowerShell,
			wantNil: true,
		},
		{
			name:     "chmod 000 (zero mode is valid)",
			script:   `chmod 000 /app/secret`,
			variant:  VariantBash,
			wantMode: 0,
			wantPath: "/app/secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
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
		{
			name:        "tee heredoc simple",
			script:      "<<EOF tee /app/config\nhello world\nEOF",
			wantPath:    "/app/config",
			wantContent: "hello world\n",
		},
		{
			name:        "tee heredoc with dash (tab stripping)",
			script:      "<<-EOF tee /app/config\n\thello\n\tworld\nEOF",
			wantPath:    "/app/config",
			wantContent: "hello\nworld\n",
		},
		{
			name:       "tee heredoc with variable - unsafe",
			script:     "<<EOF tee /app/config\nhello $USER\nEOF",
			wantPath:   "/app/config",
			wantUnsafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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

func TestDetectFileCreationPrintfEscapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		script      string
		wantNil     bool
		wantPath    string
		wantContent string
		wantUnsafe  bool
	}{
		{
			name:        "printf with newline escapes",
			script:      `printf '#ifndef H\n#define H\n#endif\n' > /usr/include/h.h`,
			wantPath:    "/usr/include/h.h",
			wantContent: "#ifndef H\n#define H\n#endif\n",
		},
		{
			name:        "printf with tab and newline escapes",
			script:      `printf 'key:\n\tvalue\n' > /app/config.yml`,
			wantPath:    "/app/config.yml",
			wantContent: "key:\n\tvalue\n",
		},
		{
			name:        "printf with backslash escape",
			script:      `printf 'back\\slash\n' > /app/file`,
			wantPath:    "/app/file",
			wantContent: "back\\slash\n",
		},
		{
			name:        "printf percent-s with newline escape",
			script:      `printf '%s\n' 'hello world' > /app/file`,
			wantPath:    "/app/file",
			wantContent: "hello world\n",
		},
		{
			name:        "printf with literal percent via %%",
			script:      `printf '100%% done\n' > /app/file`,
			wantPath:    "/app/file",
			wantContent: "100% done\n",
		},
		{
			name:       "printf with unsupported octal escape",
			script:     `printf '\101\n' > /app/file`,
			wantPath:   "/app/file",
			wantUnsafe: true,
		},
		{
			name:       "printf with %d format specifier - unsupported",
			script:     `printf '%d\n' 42 > /app/file`,
			wantPath:   "/app/file",
			wantUnsafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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

func TestProcessPrintfEscapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		want   string
		wantOk bool
	}{
		{"no escapes", "hello", "hello", true},
		{"newline", `line1\nline2`, "line1\nline2", true},
		{"tab", `col1\tcol2`, "col1\tcol2", true},
		{"backslash", `path\\dir`, "path\\dir", true},
		{"carriage return", `line\r\n`, "line\r\n", true},
		{"multiple newlines", `a\nb\nc\n`, "a\nb\nc\n", true},
		{"unsupported octal", `\101`, "", false},
		{"unsupported hex", `\x41`, "", false},
		{"trailing backslash", `hello\`, "", false},
		{"empty string", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := processPrintfEscapes(tt.input)
			if ok != tt.wantOk {
				t.Errorf("processPrintfEscapes(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Errorf("processPrintfEscapes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectFileCreationEchoEdgeCases(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestDetectFileCreationEchoEscapesByShell(t *testing.T) {
	t.Parallel()

	script := `echo "#! /bin/bash\n\n# script to activate the conda environment" > /root/.bashrc`

	t.Run("plain echo stays literal by default", func(t *testing.T) {
		t.Parallel()

		result := DetectFileCreation(script, VariantPOSIX, nil)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		want := "#! /bin/bash\\n\\n# script to activate the conda environment\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
	})

	t.Run("plain echo escapes can be interpreted when enabled", func(t *testing.T) {
		t.Parallel()

		result := DetectFileCreationWithOptions(script, VariantPOSIX, nil, FileCreationOptions{
			InterpretPlainEchoEscapes: true,
		})
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		want := "#! /bin/bash\n\n# script to activate the conda environment\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
	})
}

func TestDetectFileCreationWithUmask(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestDetectFileCreationPipeToTee(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		script      string
		wantNil     bool
		wantPath    string
		wantContent string
		wantAppend  bool
	}{
		{
			name:        "single echo piped to tee",
			script:      `echo 'hello' | tee /etc/config`,
			wantPath:    "/etc/config",
			wantContent: "hello\n",
		},
		{
			name:        "brace group of echos piped to tee",
			script:      `{ echo 'a'; echo 'b'; echo 'c'; } | tee /etc/config`,
			wantPath:    "/etc/config",
			wantContent: "a\nb\nc\n",
		},
		{
			name:        "brace group piped to tee -a",
			script:      `{ echo 'a'; echo 'b'; } | tee -a /etc/config`,
			wantPath:    "/etc/config",
			wantContent: "a\nb\n",
			wantAppend:  true,
		},
		{
			name:        "pipe tee with stdout /dev/null",
			script:      `{ echo 'x'; } | tee /etc/config > /dev/null`,
			wantPath:    "/etc/config",
			wantContent: "x\n",
		},
		{
			name:    "pipe to tee with relative path - skip",
			script:  `{ echo 'x'; } | tee config.txt`,
			wantNil: true,
		},
		{
			name:    "pipe to non-tee command",
			script:  `{ echo 'x'; } | cat > /etc/config`,
			wantNil: true,
		},
		{
			name:        "brace group of printf piped to tee",
			script:      `{ printf 'a\n'; printf 'b\n'; } | tee /etc/config`,
			wantPath:    "/etc/config",
			wantContent: "a\nb\n",
		},
		{
			name:        "brace group mixing echo and printf",
			script:      `{ echo 'a'; printf 'b\n'; echo 'c'; } | tee /etc/config`,
			wantPath:    "/etc/config",
			wantContent: "a\nb\nc\n",
		},
		{
			name:        "cat heredoc piped to tee",
			script:      "cat <<EOF | tee /etc/config\nhello\nworld\nEOF",
			wantPath:    "/etc/config",
			wantContent: "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			if result.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", result.Content, tt.wantContent)
			}
			if result.IsAppend != tt.wantAppend {
				t.Errorf("IsAppend = %v, want %v", result.IsAppend, tt.wantAppend)
			}
		})
	}
}

func TestDetectFileCreations_MultiTarget(t *testing.T) {
	t.Parallel()

	script := `set -ex ` +
		`&& { echo 'a=1'; echo 'b=2'; } | tee /etc/one.conf ` +
		`&& mkdir -p /var/app ` +
		`&& { echo 'x'; } | tee /var/app/config ` +
		`&& { echo 'y'; } | tee /etc/three.ini`

	got := DetectFileCreations(script, VariantBash, nil, FileCreationOptions{})
	if got == nil {
		t.Fatal("expected non-nil MultiFileCreationInfo")
	}
	if got.HasUnsafeVariables {
		t.Errorf("HasUnsafeVariables = true, want false")
	}

	wantTargets := []string{"/etc/one.conf", "/var/app/config", "/etc/three.ini"}
	if len(got.Slots) != len(wantTargets) {
		t.Fatalf("got %d slots, want %d", len(got.Slots), len(wantTargets))
	}
	for i, want := range wantTargets {
		if got.Slots[i].Info.TargetPath != want {
			t.Errorf("slot[%d] TargetPath = %q, want %q", i, got.Slots[i].Info.TargetPath, want)
		}
	}
	if got := got.Slots[0].Info.Content; got != "a=1\nb=2\n" {
		t.Errorf("slot[0] Content = %q, want %q", got, "a=1\nb=2\n")
	}

	// mkdir -p should be recognized so callers can elide it.
	var sawMkdir bool
	for _, c := range got.Commands {
		if c.Kind == MultiCmdMkdirP && c.MkdirTarget == "/var/app" {
			sawMkdir = true
		}
	}
	if !sawMkdir {
		t.Errorf("expected a MultiCmdMkdirP for /var/app in commands")
	}
}

func TestDetectMkdirP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text       string
		wantTarget string
		wantOk     bool
	}{
		{"mkdir -p /var/app", "/var/app", true},
		{"mkdir --parents /var/app", "/var/app", true},
		{"mkdir /var/app", "", false},                // missing -p
		{"mkdir -p /a /b", "", false},                // multiple targets
		{"mkdir -p var/app", "", false},              // relative path
		{"mkdir -p --mode=0755 /var/app", "", false}, // unsupported flag
		{"mkdir -p \"/var/app\"", "", false},         // quoted arg
		{"echo hi", "", false},                       // not mkdir
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()
			target, ok := detectMkdirP(tt.text)
			if ok != tt.wantOk || target != tt.wantTarget {
				t.Errorf("detectMkdirP(%q) = (%q, %v), want (%q, %v)", tt.text, target, ok, tt.wantTarget, tt.wantOk)
			}
		})
	}
}

func TestParseUmask(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
