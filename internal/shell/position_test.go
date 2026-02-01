package shell

import (
	"testing"
)

func TestFindCommandOccurrences_Simple(t *testing.T) {
	script := "apt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(occurrences))
	}

	occ := occurrences[0]
	if occ.Name != "apt" {
		t.Errorf("Name = %q, want %q", occ.Name, "apt")
	}
	if occ.Subcommand != "install" {
		t.Errorf("Subcommand = %q, want %q", occ.Subcommand, "install")
	}
	if occ.StartCol != 0 {
		t.Errorf("StartCol = %d, want 0", occ.StartCol)
	}
	if occ.EndCol != 3 {
		t.Errorf("EndCol = %d, want 3", occ.EndCol)
	}
	if occ.Line != 0 {
		t.Errorf("Line = %d, want 0", occ.Line)
	}
}

func TestFindCommandOccurrences_WithPath(t *testing.T) {
	script := "/usr/bin/apt-get install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(occurrences))
	}

	occ := occurrences[0]
	if occ.Name != "apt-get" {
		t.Errorf("Name = %q, want %q", occ.Name, "apt-get")
	}
}

func TestFindCommandOccurrences_Piped(t *testing.T) {
	script := "apt update && apt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	if len(occurrences) != 2 {
		t.Fatalf("got %d occurrences, want 2", len(occurrences))
	}

	// First apt
	if occurrences[0].Name != "apt" {
		t.Errorf("First Name = %q, want %q", occurrences[0].Name, "apt")
	}
	if occurrences[0].Subcommand != "update" {
		t.Errorf("First Subcommand = %q, want %q", occurrences[0].Subcommand, "update")
	}
	if occurrences[0].StartCol != 0 {
		t.Errorf("First StartCol = %d, want 0", occurrences[0].StartCol)
	}

	// Second apt
	if occurrences[1].Name != "apt" {
		t.Errorf("Second Name = %q, want %q", occurrences[1].Name, "apt")
	}
	if occurrences[1].Subcommand != "install" {
		t.Errorf("Second Subcommand = %q, want %q", occurrences[1].Subcommand, "install")
	}
	if occurrences[1].StartCol != 14 {
		t.Errorf("Second StartCol = %d, want 14", occurrences[1].StartCol)
	}
}

func TestFindCommandOccurrences_MultiLine(t *testing.T) {
	script := "apt update\napt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	if len(occurrences) != 2 {
		t.Fatalf("got %d occurrences, want 2", len(occurrences))
	}

	// First apt - line 0
	if occurrences[0].Line != 0 {
		t.Errorf("First Line = %d, want 0", occurrences[0].Line)
	}

	// Second apt - line 1
	if occurrences[1].Line != 1 {
		t.Errorf("Second Line = %d, want 1", occurrences[1].Line)
	}
	if occurrences[1].StartCol != 0 {
		t.Errorf("Second StartCol = %d, want 0", occurrences[1].StartCol)
	}
}

func TestFindCommandOccurrences_WithEnvWrapper(t *testing.T) {
	script := "env apt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	// Should find both "env" and "apt"
	names := make([]string, len(occurrences))
	for i, occ := range occurrences {
		names[i] = occ.Name
	}

	if len(occurrences) < 2 {
		t.Fatalf("got %d occurrences, want at least 2: %v", len(occurrences), names)
	}

	// Find apt occurrence
	var aptOcc *CommandOccurrence
	for i := range occurrences {
		if occurrences[i].Name == "apt" {
			aptOcc = &occurrences[i]
			break
		}
	}
	if aptOcc == nil {
		t.Fatal("apt not found in occurrences")
	}
	if aptOcc.Subcommand != "install" {
		t.Errorf("apt Subcommand = %q, want %q", aptOcc.Subcommand, "install")
	}
}

func TestFindCommandOccurrence_NotFound(t *testing.T) {
	script := "apt-get install curl"
	occ := FindCommandOccurrence(script, "apt", VariantBash)

	if occ != nil {
		t.Errorf("expected nil, got %+v", occ)
	}
}

func TestFindCommandOccurrence_Found(t *testing.T) {
	script := "apt install curl && wget http://example.com"
	occ := FindCommandOccurrence(script, "apt", VariantBash)

	if occ == nil {
		t.Fatal("expected to find apt")
	}
	if occ.Name != "apt" {
		t.Errorf("Name = %q, want %q", occ.Name, "apt")
	}
}

func TestFindAllCommandOccurrences(t *testing.T) {
	script := "apt update && apt install curl"
	occs := FindAllCommandOccurrences(script, "apt", VariantBash)

	if len(occs) != 2 {
		t.Fatalf("got %d occurrences, want 2", len(occs))
	}
}

func TestFindCommandOccurrences_SubcommandWithFlags(t *testing.T) {
	script := "apt install -y curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	if len(occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(occurrences))
	}

	occ := occurrences[0]
	// Subcommand should be "install", not "-y"
	if occ.Subcommand != "install" {
		t.Errorf("Subcommand = %q, want %q", occ.Subcommand, "install")
	}
}

func TestFindCommandOccurrences_ParseError(t *testing.T) {
	// Invalid shell syntax
	script := "apt install ${"
	occurrences := FindCommandOccurrences(script, VariantBash)

	// Should return empty for invalid syntax
	if len(occurrences) > 0 {
		t.Errorf("expected empty for invalid syntax, got %d occurrences", len(occurrences))
	}
}

// TestFindCommandOccurrences_EnvWithUnsetFlag tests that env wrapper correctly
// skips the value of --unset flag and finds the wrapped command.
func TestFindCommandOccurrences_EnvWithUnsetFlag(t *testing.T) {
	// The --unset/-u flag takes a value, so "VAR" should NOT be treated as a command
	script := "env --unset VAR apt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	names := make([]string, len(occurrences))
	for i, occ := range occurrences {
		names[i] = occ.Name
	}

	// Verify apt is found (not just env)
	var aptOcc *CommandOccurrence
	for i := range occurrences {
		if occurrences[i].Name == "apt" {
			aptOcc = &occurrences[i]
			break
		}
	}
	if aptOcc == nil {
		t.Fatalf("apt not found in occurrences: %v", names)
	}
	if aptOcc.Subcommand != "install" {
		t.Errorf("apt Subcommand = %q, want %q", aptOcc.Subcommand, "install")
	}

	// Verify VAR is NOT treated as a command
	for _, occ := range occurrences {
		if occ.Name == "VAR" {
			t.Errorf("'VAR' should not be identified as a command, found: %+v", occ)
		}
	}
}

func TestFindCommandOccurrences_EnvWithShortUnsetFlag(t *testing.T) {
	// The -u flag takes a value, so "PATH" should NOT be treated as a command
	script := "env -u PATH apt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	names := make([]string, len(occurrences))
	for i, occ := range occurrences {
		names[i] = occ.Name
	}

	// Verify apt is found
	var aptOcc *CommandOccurrence
	for i := range occurrences {
		if occurrences[i].Name == "apt" {
			aptOcc = &occurrences[i]
			break
		}
	}
	if aptOcc == nil {
		t.Fatalf("apt not found in occurrences: %v", names)
	}

	// Verify PATH is NOT treated as a command
	for _, occ := range occurrences {
		if occ.Name == "PATH" {
			t.Errorf("'PATH' should not be identified as a command, found: %+v", occ)
		}
	}
}

func TestFindCommandOccurrences_NiceWithAdjustment(t *testing.T) {
	// The -n/--adjustment flag takes a value
	script := "nice -n 10 apt install curl"
	occurrences := FindCommandOccurrences(script, VariantBash)

	names := make([]string, len(occurrences))
	for i, occ := range occurrences {
		names[i] = occ.Name
	}

	// Verify apt is found (not just nice, and "10" is not a command)
	var aptOcc *CommandOccurrence
	for i := range occurrences {
		if occurrences[i].Name == "apt" {
			aptOcc = &occurrences[i]
			break
		}
	}
	if aptOcc == nil {
		t.Fatalf("apt not found in occurrences: %v", names)
	}

	// Verify "10" is NOT treated as a command
	for _, occ := range occurrences {
		if occ.Name == "10" {
			t.Errorf("'10' should not be identified as a command, found: %+v", occ)
		}
	}
}
