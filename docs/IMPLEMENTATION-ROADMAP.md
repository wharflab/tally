# Implementation Roadmap

**Prioritized action plan based on architectural research**

This roadmap provides the next 10 critical steps to transform tally from a single-rule demo into a production-ready Dockerfile linter.

---

## Priority 1: Restructure Rule System

**Goal:** Establish scalable rule architecture

**Actions:**

1. Create `internal/rules/` directory structure:

   ```text
   internal/rules/
   ├── registry.go          # Rule registration
   ├── rule.go              # Rule interface
   └── style/
       ├── max_lines.go     # Move existing rule here
       └── max_lines_test.go
   ```

2. Define core interfaces in `internal/rules/rule.go`:

   ```go
   type Rule struct {
       Code        string
       Name        string
       Description string
       Category    string
       Severity    Severity
       URL         string
       Check       RuleFunc
   }

   type RuleFunc func(*parser.AST, *SemanticModel) []Violation
   ```

3. Implement auto-registration pattern (init() functions)

4. Move `max-lines` rule to new structure as template

**References:**

- [06-code-organization.md](06-code-organization.md) - Section "One File Per Rule"
- [06-code-organization.md](06-code-organization.md) - Section "Rule Registry"

**Success Criteria:**

- [ ] Rule interface defined
- [ ] Registry implemented with Register() function
- [ ] max-lines rule migrated to new structure
- [ ] Tests pass

---

## Priority 2: Build Semantic Model

**Goal:** Enable advanced rules that need cross-instruction context

**Actions:**

1. Create `internal/parser/semantic.go`:

   ```go
   type SemanticModel struct {
       Stages    map[string]*Stage
       GlobalArgs map[string]*Variable
       StageVars  map[string]map[string]*Variable
       CopyFromRefs []CopyFromRef
       BaseImages []BaseImageRef
   }
   ```

2. Implement `BuildSemanticModel(ast)` function:
   - Parse FROM instructions → stages
   - Track ARG/ENV → variables with scopes
   - Collect COPY --from → cross-stage references
   - Store base images with platforms

3. Update linter to pass semantic model to rules

**References:**

- [03-parsing-and-ast.md](03-parsing-and-ast.md) - Section "Semantic Analysis"
- [01-linter-pipeline-architecture.md](01-linter-pipeline-architecture.md) - Section "2. Parsing Stage"

**Success Criteria:**

- [ ] SemanticModel struct defined
- [ ] BuildSemanticModel() implemented
- [ ] Unit tests for semantic analysis
- [ ] Can track stages, variables, and references

---

## Priority 3: Implement Inline Disable Support

**Goal:** Allow users to suppress specific violations

**Actions:**

1. Create `internal/inline/` package:
   - `directive.go` - Parse `# tally ignore=` comments
   - `filter.go` - Filter violations based on directives

2. Support syntax:

   ```dockerfile
   # tally ignore=DL3006
   # tally ignore=DL3006,DL3008
   # tally global ignore=DL3003
   # tally ignore=all
   ```

3. Add post-filtering step to linter pipeline

4. Track unused directives for warnings

**References:**

- [04-inline-disables.md](04-inline-disables.md) - Section "Recommended Implementation for Tally"
- [04-inline-disables.md](04-inline-disables.md) - Section "Full Implementation"

**Success Criteria:**

- [ ] Can parse inline directives from comments
- [ ] Filter() removes suppressed violations
- [ ] Detect unused directives
- [ ] Integration tests with inline comments

---

## Priority 4: Create Reporter Infrastructure

**Goal:** Support multiple output formats

**Actions:**

1. Create `internal/reporter/` package with interface:

   ```go
   type Reporter interface {
       Report(violations []Violation) error
   }
   ```

2. Implement initial reporters:
   - `text.go` - Human-readable colored output (use Lip Gloss)
   - `json.go` - Machine-readable structured output

3. Add factory pattern for format selection

4. Wire into CLI with `--format` flag

**References:**

- [05-reporters-and-output.md](05-reporters-and-output.md) - Section "Core Reporter Pattern"
- [05-reporters-and-output.md](05-reporters-and-output.md) - Section "Multiple Output Support"

**Success Criteria:**

- [ ] Reporter interface defined
- [ ] Text reporter with colors (Lip Gloss)
- [ ] JSON reporter
- [ ] Factory for format selection
- [ ] CLI flag `--format=text|json`

---

## Priority 5: Add File-Level Parallelism

**Goal:** Efficiently lint multiple files

**Actions:**

1. Implement worker pool in linter:

   ```go
   func (l *Linter) LintFiles(paths []string) ([]Violation, error) {
       // Use errgroup for parallel execution
       // Each file linted independently
       // Aggregate violations
   }
   ```

2. Use `golang.org/x/sync/errgroup` for coordination

3. Make worker count configurable (default: NumCPU)

4. Ensure no shared mutable state between workers

**References:**

- [01-linter-pipeline-architecture.md](01-linter-pipeline-architecture.md) - Section "3. Rule Evaluation Stage" → "Option A: File-Level Parallelism"
- [01-linter-pipeline-architecture.md](01-linter-pipeline-architecture.md) - Section "For Tally" (parallelism recommendation)

**Success Criteria:**

- [ ] Can lint multiple files in parallel
- [ ] Worker pool limits concurrency
- [ ] No race conditions (go test -race passes)
- [ ] Benchmark shows performance improvement

---

## Priority 6: Implement Top 5 Critical Rules

**Goal:** Provide immediate value with essential rules

**Actions:**
Implement these rules (one file each in appropriate category):

1. **DL3006** - Pin base image versions (`internal/rules/base/pin_version.go`)
   - Check FROM instructions lack explicit tag
   - Severity: warning

2. **DL3004** - No sudo (`internal/rules/security/no_sudo.go`)
   - Scan RUN instructions for sudo usage
   - Severity: error

3. **DL3020** - Use COPY not ADD (`internal/rules/instruction/copy_not_add.go`)
   - Check for ADD when COPY is appropriate
   - Severity: error

4. **DL3024** - Unique stage names (`internal/rules/stage/unique_names.go`)
   - Use semantic model to detect duplicates
   - Severity: error

5. **DL3002** - Don't run as root (`internal/rules/security/no_root_user.go`)
   - Check last USER instruction is not root
   - Severity: warning

Each rule needs:

- Implementation file
- Test file with table-driven tests
- Examples (good/bad)

**References:**

- [08-hadolint-rules-reference.md](08-hadolint-rules-reference.md) - Section "Critical Priority"
- [06-code-organization.md](06-code-organization.md) - Section "Rule Structure Template"

**Success Criteria:**

- [ ] All 5 rules implemented
- [ ] Unit tests for each rule
- [ ] Integration tests pass
- [ ] Rules auto-register

---

## Priority 7: Add Violation Processing Pipeline

**Goal:** Filter, deduplicate, and sort violations consistently

**Actions:**

1. Create processor chain in `internal/linter/pipeline.go`:

   ```go
   type Processor interface {
       Process(violations []Violation) ([]Violation, error)
   }
   ```

2. Implement processors:
   - `InlineDisableFilter` - Apply `# tally ignore=`
   - `Deduplicator` - Remove exact duplicates
   - `SortProcessor` - Sort by severity, file, line
   - `MaxPerFileFilter` - Limit violations per file (configurable)

3. Chain processors in linter

**References:**

- [01-linter-pipeline-architecture.md](01-linter-pipeline-architecture.md) - Section "5. Processing Pipeline"
- [04-inline-disables.md](04-inline-disables.md) - Section "Approach 3: Post-Filtering"

**Success Criteria:**

- [ ] Processor interface defined
- [ ] Core processors implemented
- [ ] Pipeline chains processors
- [ ] Violations are filtered and sorted

---

## Priority 8: Implement File Discovery

**Goal:** Find all Dockerfiles to lint

**Actions:**

1. Create `internal/discovery/` package

2. Support input types:
   - Single file: `tally check Dockerfile`
   - Directory: `tally check .` (find all Dockerfiles recursively)
   - Multiple: `tally check Dockerfile build/Dockerfile.prod`
   - Glob patterns: `tally check **/Dockerfile*`

3. Filter logic:
   - Skip hidden directories (unless explicit)
   - Respect .gitignore (optional flag)
   - Default Dockerfile patterns: `Dockerfile`, `Dockerfile.*`, `*.Dockerfile`

4. Add `--exclude` flag for patterns

**References:**

- [01-linter-pipeline-architecture.md](01-linter-pipeline-architecture.md) - Section "1. File Discovery Stage"

**Success Criteria:**

- [ ] Can discover files from various inputs
- [ ] Recursive directory search works
- [ ] Glob patterns supported
- [ ] Exclusion patterns work

---

## Priority 9: Add SARIF Reporter

**Goal:** Enable CI/CD integration

**Actions:**

1. Add dependency: `go get github.com/owenrumney/go-sarif/v2`

2. Implement `internal/reporter/sarif.go`:
   - Convert violations to SARIF results
   - Include rule metadata
   - Add source locations with ranges
   - Link to documentation URLs

3. Add to reporter factory

4. Test with GitHub Actions (optional)

**References:**

- [05-reporters-and-output.md](05-reporters-and-output.md) - Section "3. SARIF Format"
- [05-reporters-and-output.md](05-reporters-and-output.md) - Section "Recommended Libraries"

**Success Criteria:**

- [ ] SARIF reporter implemented
- [ ] Valid SARIF 2.1.0 output
- [ ] Can be consumed by GitHub Actions
- [ ] Documentation URLs included

---

## Priority 10: Enhance Integration Tests

**Goal:** Ensure end-to-end correctness

**Actions:**

1. Expand `internal/integration/testdata/` with fixtures:

   ```text
   testdata/
   ├── critical_violations/
   │   └── Dockerfile (triggers DL3004, DL3020)
   ├── multi_stage/
   │   └── Dockerfile (tests stage rules)
   ├── with_inline_disables/
   │   └── Dockerfile (tests # tally ignore=)
   └── clean/
       └── Dockerfile (no violations)
   ```

2. Add snapshot tests for each fixture:
   - JSON output
   - Violation counts
   - Rule codes triggered

3. Test all reporters:
   - Text output
   - JSON output
   - SARIF output

4. Add `make test-integration` target

**References:**

- [06-code-organization.md](06-code-organization.md) - Section "Testing Strategy" → "Integration Tests"
- Current: `internal/integration/integration_test.go`

**Success Criteria:**

- [ ] 5+ integration test fixtures
- [ ] Snapshot tests with go-snaps
- [ ] All reporters tested end-to-end
- [ ] `UPDATE_SNAPS=true make test` updates snapshots

---

## Implementation Notes

### Order Dependencies

- **1 → 2**: Need rule system before semantic model
- **2 → 6**: Need semantic model before implementing stage rules
- **1, 4 → 7**: Need rules and reporters before pipeline
- **3 → 7**: Inline disables integrated into pipeline
- **1-7 → 10**: Need core functionality before comprehensive tests

### Key Design Principles

1. **Incremental value**: Each step produces working software
2. **Test-driven**: Add tests alongside implementation
3. **Avoid rewrites**: Build on existing buildkit parser
4. **Simple first**: Start with synchronous, add parallelism later
5. **Real-world ready**: Focus on rules users actually need

### Post-Priority 10

After completing these 10 priorities, tally will have:

- ✅ Scalable rule system
- ✅ Semantic analysis
- ✅ Inline disables
- ✅ Multiple output formats
- ✅ Parallel execution
- ✅ 5 critical rules
- ✅ Processing pipeline
- ✅ File discovery
- ✅ CI/CD integration (SARIF)
- ✅ Comprehensive tests

**Next phases** (reference [08-hadolint-rules-reference.md](08-hadolint-rules-reference.md)):

- Phase 2: Add 15 high-priority rules (package managers, multi-stage)
- Phase 3: Add 20 medium-priority rules (best practices)
- Phase 4: Context-aware rules ([07-context-aware-foundation.md](07-context-aware-foundation.md))

---

## Quick Reference

| Priority | Focus | Key File(s) | Doc Reference |
|----------|-------|-------------|---------------|
| 1 | Rule system | `internal/rules/` | [06](06-code-organization.md) |
| 2 | Semantic model | `internal/parser/semantic.go` | [03](03-parsing-and-ast.md) |
| 3 | Inline disables | `internal/inline/` | [04](04-inline-disables.md) |
| 4 | Reporters | `internal/reporter/` | [05](05-reporters-and-output.md) |
| 5 | Parallelism | `internal/linter/` | [01](01-linter-pipeline-architecture.md) |
| 6 | Critical rules | `internal/rules/*/` | [08](08-hadolint-rules-reference.md) |
| 7 | Pipeline | `internal/linter/pipeline.go` | [01](01-linter-pipeline-architecture.md) |
| 8 | File discovery | `internal/discovery/` | [01](01-linter-pipeline-architecture.md) |
| 9 | SARIF | `internal/reporter/sarif.go` | [05](05-reporters-and-output.md) |
| 10 | Integration tests | `internal/integration/` | [06](06-code-organization.md) |

---

## Tracking Progress

Create a GitHub project or use this checklist to track implementation:

```markdown
## v1.0 Implementation Checklist

### Foundation

- [ ] Priority 1: Restructure rule system
- [ ] Priority 2: Build semantic model
- [ ] Priority 3: Inline disable support
- [ ] Priority 4: Reporter infrastructure
- [ ] Priority 5: File-level parallelism

### Core Features

- [ ] Priority 6: Top 5 critical rules
- [ ] Priority 7: Processing pipeline
- [ ] Priority 8: File discovery
- [ ] Priority 9: SARIF reporter
- [ ] Priority 10: Integration tests

### Ready for v1.0 Release

- [ ] All priorities complete
- [ ] Documentation updated
- [ ] Examples added to README
- [ ] Release notes written
```
