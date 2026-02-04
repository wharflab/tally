package shell

import (
	"testing"
)

func TestDetectFileCreation(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		variant    Variant
		wantNil    bool
		wantPath   string
		wantChmod  string
		wantUnsafe bool
	}{
		{
			name:     "simple echo to file",
			script:   `echo "hello world" > /app/config.txt`,
			variant:  VariantBash,
			wantPath: "/app/config.txt",
		},
		{
			name:     "echo with chmod",
			script:   `echo "#!/bin/bash" > /app/script.sh && chmod 755 /app/script.sh`,
			variant:  VariantBash,
			wantPath: "/app/script.sh",
			wantChmod: "755",
		},
		{
			name:     "echo with 4-digit chmod",
			script:   `echo "data" > /app/file && chmod 0644 /app/file`,
			variant:  VariantBash,
			wantPath: "/app/file",
			wantChmod: "0644",
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
			wantChmod: "0755", // +x on 0o644 base = 0755
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
		{"+x", false},
		{"u+rwx", false},
		{"a+r", false},
		{"", false},
		{"888", false}, // Invalid octal
		{"75", false},  // Too short
		{"07555", false}, // Too long
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
		want     string
	}{
		// Add execute
		{"+x", 0o644, "0755"},
		{"a+x", 0o644, "0755"},
		{"u+x", 0o644, "0744"},
		{"g+x", 0o644, "0654"},
		{"o+x", 0o644, "0645"},
		{"ug+x", 0o644, "0754"},

		// Add read (already has it for user)
		{"+r", 0o644, "0644"},
		{"o+r", 0o644, "0644"}, // other already has read

		// Add write
		{"+w", 0o644, "0666"},
		{"g+w", 0o644, "0664"},
		{"o+w", 0o644, "0646"},

		// Combined permissions
		{"+rwx", 0o644, "0777"},
		{"u+rwx", 0o644, "0744"}, // user already has rw, adds x
		{"go+rx", 0o644, "0655"},

		// Remove permissions
		{"-x", 0o644, "0644"},  // no execute to remove
		{"-w", 0o644, "0444"},  // removes write from user
		{"o-r", 0o644, "0640"}, // removes read from other

		// Set exactly
		{"=rwx", 0o644, "0777"},  // sets all to rwx
		{"u=rwx", 0o644, "0744"}, // sets user to rwx exactly
		{"go=rx", 0o644, "0655"}, // sets group/other to rx exactly

		// Unsupported modes (return empty)
		{"+X", 0o644, ""},
		{"+s", 0o644, ""},
		{"+t", 0o644, ""},
		{"", 0o644, ""},
	}

	for _, tt := range tests {
		t.Run(tt.symbolic, func(t *testing.T) {
			got := symbolicToOctal(tt.symbolic, tt.base)
			if got != tt.want {
				t.Errorf("symbolicToOctal(%q, %04o) = %q, want %q", tt.symbolic, tt.base, got, tt.want)
			}
		})
	}
}

func TestNormalizeOctalMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"755", "0755"},
		{"0755", "0755"},
		{"644", "0644"},
		{"0644", "0644"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeOctalMode(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeOctalMode(%q) = %q, want %q", tt.input, got, tt.want)
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
		wantMode string
		wantPath string
	}{
		{
			name:     "simple chmod 755",
			script:   `chmod 755 /app/script.sh`,
			variant:  VariantBash,
			wantMode: "755",
			wantPath: "/app/script.sh",
		},
		{
			name:     "chmod 0644",
			script:   `chmod 0644 /app/config`,
			variant:  VariantBash,
			wantMode: "0644",
			wantPath: "/app/config",
		},
		{
			name:     "chmod +x (converted to octal)",
			script:   `chmod +x /app/run.sh`,
			variant:  VariantBash,
			wantMode: "0755", // +x on base 0644 = 0755
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
				t.Errorf("Mode = %q, want %q", result.Mode, tt.wantMode)
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
			name:     "cat heredoc with variable - unsafe",
			script:   "cat <<EOF > /app/config\nhello $USER\nEOF",
			wantPath: "/app/config",
			// HasUnsafeVariables should be true
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
	}{
		{
			name:        "echo with no arguments",
			script:      `echo > /app/file`,
			wantPath:    "/app/file",
			wantContent: "\n",
		},
		{
			name:     "echo -e with escape",
			script:   `echo -e "hello\tworld" > /app/file`,
			wantPath: "/app/file",
			// HasUnsafeVariables should be true due to -e
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
		})
	}
}
