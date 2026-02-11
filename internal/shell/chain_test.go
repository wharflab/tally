package shell

import (
	"slices"
	"testing"
)

func TestFindCommandInChain_Standalone(t *testing.T) {
	t.Parallel()
	pos := FindCommandInChain("ln -sf /bin/bash /bin/sh", VariantBash, func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	})
	if pos == nil {
		t.Fatal("expected to find command")
	}
	if !pos.IsStandalone {
		t.Error("expected IsStandalone to be true")
	}
	if pos.HasOtherStatements {
		t.Error("expected HasOtherStatements to be false")
	}
	if pos.PrecedingCommands != "" {
		t.Errorf("expected empty PrecedingCommands, got %q", pos.PrecedingCommands)
	}
	if pos.RemainingCommands != "" {
		t.Errorf("expected empty RemainingCommands, got %q", pos.RemainingCommands)
	}
}

func TestFindCommandInChain_AtEnd(t *testing.T) {
	t.Parallel()
	pos := FindCommandInChain("apt-get update && ln -sf /bin/bash /bin/sh", VariantBash, func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	})
	if pos == nil {
		t.Fatal("expected to find command")
	}
	if pos.IsStandalone {
		t.Error("expected IsStandalone to be false")
	}
	if pos.PrecedingCommands != "apt-get update" {
		t.Errorf("PrecedingCommands = %q, want %q", pos.PrecedingCommands, "apt-get update")
	}
	if pos.RemainingCommands != "" {
		t.Errorf("expected empty RemainingCommands, got %q", pos.RemainingCommands)
	}
}

func TestFindCommandInChain_AtStart(t *testing.T) {
	t.Parallel()
	pos := FindCommandInChain("ln -sf /bin/bash /bin/sh && echo done", VariantBash, func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	})
	if pos == nil {
		t.Fatal("expected to find command")
	}
	if pos.PrecedingCommands != "" {
		t.Errorf("expected empty PrecedingCommands, got %q", pos.PrecedingCommands)
	}
	if pos.RemainingCommands != "echo done" {
		t.Errorf("RemainingCommands = %q, want %q", pos.RemainingCommands, "echo done")
	}
}

func TestFindCommandInChain_InMiddle(t *testing.T) {
	t.Parallel()
	pos := FindCommandInChain(
		"apt-get update && ln -sf /bin/bash /bin/sh && echo done",
		VariantBash,
		func(name string, args []string) bool {
			return name == "ln" && slices.Contains(args, "/bin/sh")
		},
	)
	if pos == nil {
		t.Fatal("expected to find command")
	}
	if pos.PrecedingCommands != "apt-get update" {
		t.Errorf("PrecedingCommands = %q, want %q", pos.PrecedingCommands, "apt-get update")
	}
	if pos.RemainingCommands != "echo done" {
		t.Errorf("RemainingCommands = %q, want %q", pos.RemainingCommands, "echo done")
	}
}

// Semicolons create separate top-level statements in the shell AST.
// The command is found but PrecedingCommands/RemainingCommands only
// cover the && chain within the same statement, not across semicolons.
func TestFindCommandInChain_Semicolon(t *testing.T) {
	t.Parallel()
	lnMatcher := func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	}

	t.Run("at start with semicolon", func(t *testing.T) {
		t.Parallel()
		pos := FindCommandInChain("ln -sf /bin/bash /bin/sh; echo done", VariantBash, lnMatcher)
		if pos == nil {
			t.Fatal("expected to find command")
		}
		if pos.IsStandalone {
			t.Error("expected IsStandalone to be false (multiple top-level statements)")
		}
		if !pos.HasOtherStatements {
			t.Error("expected HasOtherStatements to be true")
		}
	})

	t.Run("after semicolon", func(t *testing.T) {
		t.Parallel()
		pos := FindCommandInChain("cmd1; ln -sf /bin/bash /bin/sh", VariantBash, lnMatcher)
		if pos == nil {
			t.Fatal("expected to find command")
		}
		if pos.IsStandalone {
			t.Error("expected IsStandalone to be false")
		}
		if !pos.HasOtherStatements {
			t.Error("expected HasOtherStatements to be true")
		}
	})

	t.Run("mixed && and semicolon", func(t *testing.T) {
		t.Parallel()
		pos := FindCommandInChain(
			"cmd1 && cmd2; ln -sf /bin/bash /bin/sh",
			VariantBash,
			lnMatcher,
		)
		if pos == nil {
			t.Fatal("expected to find command")
		}
		if !pos.HasOtherStatements {
			t.Error("expected HasOtherStatements to be true")
		}
	})
}

func TestFindCommandInChain_Pipe(t *testing.T) {
	t.Parallel()
	lnMatcher := func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	}

	// Pipes create BinaryCmd nodes just like &&. The command is found on
	// the right side of the pipe. The operator should be preserved.
	pos := FindCommandInChain("cmd1 | ln -sf /bin/bash /bin/sh", VariantBash, lnMatcher)
	if pos == nil {
		t.Fatal("expected to find command in pipe")
	}
	if pos.PrecedingCommands != "cmd1" {
		t.Errorf("PrecedingCommands = %q, want %q", pos.PrecedingCommands, "cmd1")
	}
}

func TestFindCommandInChain_OrOperator(t *testing.T) {
	t.Parallel()
	lnMatcher := func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	}

	// || operator should be preserved in context strings.
	pos := FindCommandInChain("cmd1 || ln -sf /bin/bash /bin/sh || echo done", VariantBash, lnMatcher)
	if pos == nil {
		t.Fatal("expected to find command")
	}
	if pos.PrecedingCommands != "cmd1" {
		t.Errorf("PrecedingCommands = %q, want %q", pos.PrecedingCommands, "cmd1")
	}
	if pos.RemainingCommands != "echo done" {
		t.Errorf("RemainingCommands = %q, want %q", pos.RemainingCommands, "echo done")
	}
}

func TestFindCommandInChain_MixedOperators(t *testing.T) {
	t.Parallel()
	lnMatcher := func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	}

	// Mixed operators: the context should preserve the original operators.
	pos := FindCommandInChain(
		"cmd1 && ln -sf /bin/bash /bin/sh || echo fallback",
		VariantBash,
		lnMatcher,
	)
	if pos == nil {
		t.Fatal("expected to find command")
	}
	if pos.PrecedingCommands != "cmd1" {
		t.Errorf("PrecedingCommands = %q, want %q", pos.PrecedingCommands, "cmd1")
	}
	if pos.RemainingCommands != "echo fallback" {
		t.Errorf("RemainingCommands = %q, want %q", pos.RemainingCommands, "echo fallback")
	}
}

func TestFindCommandInChain_NoMatch(t *testing.T) {
	t.Parallel()
	pos := FindCommandInChain("apt-get update && echo hello", VariantBash, func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	})
	if pos != nil {
		t.Error("expected nil when no command matches")
	}
}

func TestFindCommandInChain_MatchesOnlyPredicatedArgs(t *testing.T) {
	t.Parallel()
	// ln without /bin/sh should not match
	pos := FindCommandInChain("ln -sf /bin/true /sbin/initctl", VariantBash, func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	})
	if pos != nil {
		t.Error("expected nil when ln does not target /bin/sh")
	}
}
