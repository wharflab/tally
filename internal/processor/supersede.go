package processor

import (
	"path/filepath"

	"github.com/wharflab/tally/internal/rules"
)

// Supersession suppresses lower-severity violations when an error-level
// violation exists at the same location (file + line). This handles
// cross-rule interactions where a cosmetic suggestion (e.g. StageNameCasing)
// is meaningless when an error (e.g. ReservedStageName) already flags the line.
type Supersession struct{}

// NewSupersession creates a new supersession processor.
func NewSupersession() *Supersession {
	return &Supersession{}
}

// Name returns the processor's identifier.
func (p *Supersession) Name() string {
	return "supersession"
}

// Process removes violations that are superseded by a higher-severity
// violation at the same file+line. Only error-level violations suppress
// lower-severity ones.
func (p *Supersession) Process(violations []rules.Violation, _ *Context) []rules.Violation {
	type locKey struct {
		file string
		line int
	}

	// Collect locations that have at least one error-level violation.
	errorLocations := make(map[locKey]struct{})
	for _, v := range violations {
		if v.Severity == rules.SeverityError {
			if v.Location.File == "" || v.Location.Start.Line <= 0 {
				continue
			}
			errorLocations[locKey{
				file: filepath.ToSlash(v.Location.File),
				line: v.Location.Start.Line,
			}] = struct{}{}
		}
	}

	if len(errorLocations) == 0 {
		return violations
	}

	return filterViolations(violations, func(v rules.Violation) bool {
		if v.Severity == rules.SeverityError {
			return true
		}
		if v.Location.File == "" || v.Location.Start.Line <= 0 {
			return true
		}
		key := locKey{
			file: filepath.ToSlash(v.Location.File),
			line: v.Location.Start.Line,
		}
		_, hasError := errorLocations[key]
		return !hasError
	})
}
