package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/mattn/go-isatty"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
)

func planAcpFixSpinner(
	violations []rules.Violation,
	safetyThreshold fix.FixSafety,
	ruleFilter []string,
	fixModes map[string]map[string]fix.FixMode,
	fileConfigs map[string]*config.Config,
) (int, time.Duration) {
	normalizedConfigs := make(map[string]*config.Config, len(fileConfigs))
	for path, cfg := range fileConfigs {
		normalizedConfigs[filepath.Clean(path)] = cfg
	}

	count := 0
	maxTimeout := time.Duration(0)

	for _, v := range violations {
		sf := v.SuggestedFix
		if sf == nil || !sf.NeedsResolve {
			continue
		}
		if sf.ResolverID != autofixdata.ResolverID {
			continue
		}
		if sf.Safety > safetyThreshold {
			continue
		}
		if len(ruleFilter) > 0 && !containsString(ruleFilter, v.RuleCode) {
			continue
		}
		if !fixModeAllowed(fixModes, safetyThreshold, ruleFilter, v.File(), v.RuleCode) {
			continue
		}

		cfg := normalizedConfigs[filepath.Clean(v.File())]
		if cfg == nil || !cfg.AI.Enabled || len(cfg.AI.Command) == 0 {
			continue
		}

		count++
		if d, err := time.ParseDuration(cfg.AI.Timeout); err == nil && d > maxTimeout {
			maxTimeout = d
		}
	}

	return count, maxTimeout
}

func startAcpFixSpinner(count int, timeout time.Duration) func() {
	if count <= 0 {
		return func() {}
	}

	msg := acpSpinnerMessage(count, timeout)
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		// Non-interactive: avoid spinners; leave a single note.
		_, _ = fmt.Fprintln(os.Stderr, msg)
		return func() {}
	}

	sp := spinner.Line
	frames := sp.Frames
	interval := sp.FPS
	if len(frames) == 0 {
		frames = []string{"-"}
	}
	if interval <= 0 {
		interval = 120 * time.Millisecond
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		frame := 0
		for {
			select {
			case <-stop:
				// Clear the line so subsequent output starts cleanly.
				_, _ = fmt.Fprint(os.Stderr, "\r\033[2K")
				close(done)
				return
			case <-ticker.C:
				_, _ = fmt.Fprintf(os.Stderr, "\r%s %s", frames[frame%len(frames)], msg)
				frame++
			}
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}

func acpSpinnerMessage(count int, timeout time.Duration) string {
	word := "fix"
	if count != 1 {
		word = "fixes"
	}
	msg := fmt.Sprintf("Waiting for %d AI ACP %s in progress", count, word)
	if timeout > 0 {
		msg += fmt.Sprintf(" (timeout: %s)", humanizeTimeout(timeout))
	}
	return msg
}

func humanizeTimeout(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d <= 0 {
		return "0 seconds"
	}
	if d < time.Minute && d%time.Second == 0 {
		secs := int(d.Seconds())
		if secs == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", secs)
	}
	if d%time.Minute == 0 {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	return d.String()
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func fixModeAllowed(
	fixModes map[string]map[string]fix.FixMode,
	safetyThreshold fix.FixSafety,
	ruleFilter []string,
	filePath string,
	ruleCode string,
) bool {
	mode := fix.FixModeAlways
	if fixModes != nil {
		if fileModes, ok := fixModes[filepath.Clean(filePath)]; ok {
			if m, ok := fileModes[ruleCode]; ok {
				mode = m
			}
		}
	}

	switch mode {
	case config.FixModeNever:
		return false
	case config.FixModeExplicit:
		return len(ruleFilter) > 0 && containsString(ruleFilter, ruleCode)
	case config.FixModeUnsafeOnly:
		return safetyThreshold >= fix.FixUnsafe
	case config.FixModeAlways:
		return true
	default:
		return true
	}
}
