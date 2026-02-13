# Hadolint Rules Reference

**Source:** <https://github.com/hadolint/hadolint> (v2.12+)

Complete reference of Hadolint's 70 rules for implementation roadmap.

---

## Rule Categories

- **DL1xxx**: Meta rules (1 rule)
- **DL3xxx**: Best practices (61 rules)
- **DL4xxx**: Maintainability (8 rules)
- **SCxxxx**: ShellCheck integration (dozens of shell-specific rules)

---

## Severity Levels

| Severity | Count | Meaning |
|----------|-------|---------|
| **error** | 18 | Critical issues that will likely cause build/runtime failures |
| **warning** | 35 | Important issues affecting security, performance, or maintainability |
| **info** | 14 | Helpful suggestions for best practices |
| **style** | 1 | Cosmetic issues |
| **ignore** | 2 | Disabled by default |

**Note:** All severities are configurable via config file or CLI.

---

## Complete Rule List

### Meta Rules (DL1xxx)

| Code | Name | Severity | Description |
|------|------|----------|-------------|
| **DL1001** | Invalid instruction | error | Instruction is not valid |

---

### Best Practices: Base Images (DL30xx)

| Code | Name | Severity | Description |
|------|------|----------|-------------|
| **DL3000** | Absolute WORKDIR | error | Use absolute WORKDIR |
| **DL3001** | Deleting apt lists | info | Deleting lists should be in same RUN as apt-get |
| **DL3002** | Switch to non-root | warning | Last USER should not be root |
| **DL3003** | Use WORKDIR | warning | Use WORKDIR to switch to a directory |
| **DL3004** | No sudo | error | Do not use sudo as it has unpredictable behavior |
| **DL3006** | Pin versions | warning | Always tag the version of an image explicitly |
| **DL3007** | Avoid :latest | warning | Using latest is prone to errors |
| **DL3008** | Pin apt versions | warning | Pin versions in apt-get install |
| **DL3009** | Clean apt cache | info | Delete apt-get lists after installing |
| **DL3010** | Use ADD for archives | info | Use ADD for extracting archives into an image |
| **DL3011** | Valid port | error | Valid UNIX ports range from 0 to 65535 |
| **DL3012** | HEALTHCHECK args | error | Multiple HEALTHCHECK instructions |
| **DL3013** | Pin pip versions | warning | Pin versions in pip install |
| **DL3014** | Use -y with apt-get | warning | Use the -y switch |
| **DL3015** | Avoid apt-get upgrade | info | Avoid using apt-get upgrade or dist-upgrade |
| **DL3016** | Pin npm versions | warning | Pin versions in npm install |
| **DL3017** | No apk upgrade | info | Do not use apk upgrade |
| **DL3018** | Pin apk versions | warning | Pin versions in apk add |
| **DL3019** | Use --no-cache with apk | info | Use --no-cache switch to avoid caching |
| **DL3020** | COPY instead of ADD | error | Use COPY instead of ADD for files/folders |
| **DL3021** | COPY --from invalid | error | COPY with more than 2 arguments requires --from |
| **DL3022** | COPY --from not found | warning | COPY --from should reference a previously defined stage |
| **DL3023** | COPY --from same stage | error | COPY --from should reference a different stage |
| **DL3024** | FROM aliases unique | error | FROM aliases (stage names) must be unique |
| **DL3025** | Use JSON for CMD/ENTRYPOINT | warning | Use arguments JSON notation for CMD/ENTRYPOINT |
| **DL3026** | Use trusted base images | error | Use only an allowed registry in FROM |
| **DL3027** | No apt-get upgrade in FROM | warning | Do not use apt-get dist-upgrade |
| **DL3028** | Pin gem versions | warning | Pin versions in gem install |
| **DL3029** | No --chown with COPY | warning | Don't use --chown with COPY from multi-stage |
| **DL3030** | Use yum-config-manager | warning | Use yum-config-manager to enable/disable repos |
| **DL3031** | Configure GPGKEY for yum | warning | Specify GPGKEY for yum package manager |
| **DL3032** | Pin yum versions | warning | yum clean all after install |
| **DL3033** | Specify version with yum | warning | Specify version with yum install |
| **DL3034** | Missing yum clean | warning | Missing yum clean all after yum command |
| **DL3035** | Avoid dist-upgrade with zypper | warning | Do not use zypper dist-upgrade |
| **DL3036** | Pin zypper versions | warning | Specify version with zypper install |
| **DL3037** | Clean zypper cache | warning | Use zypper clean after install |
| **DL3038** | Use dnf-config-manager | warning | Use dnf-config-manager to enable/disable repos |
| **DL3039** | Clean dnf cache | warning | Use dnf clean all after install |
| **DL3040** | Specify version with dnf | warning | Specify version with dnf install |
| **DL3041** | Specify GPGKEY for dnf | warning | Specify GPGKEY for dnf package manager |
| **DL3042** | Avoid cache with pip | warning | Avoid cache directory with pip install --no-cache-dir |
| **DL3043** | ONBUILD forbidden | warning | ONBUILD should be avoided |
| **DL3044** | ENV forbidden keys | error | Do not use forbidden environment variables |
| **DL3045** | COPY with more than 2 args | error | COPY with more than 2 arguments requires --from |
| **DL3046** | useradd without -l | warning | useradd without -l creates large UID in log |
| **DL3047** | Use && after cd | warning | wget or curl should be followed by cleanup |
| **DL3048** | Invalid LABEL format | style | Invalid LABEL format |
| **DL3049** | LABEL deprecation | info | Label 'maintainer' is deprecated (use LABEL) |
| **DL3050** | Too many COPY/ADD layers | info | Too many COPY/ADD layers |
| **DL3051** | LABEL schema invalid | warning | Label schema does not match schema.org |
| **DL3052** | LABEL contains URL | warning | Avoid URLs in LABEL values |
| **DL3055** | Use LABEL-schema | info | Use LABEL-schema namespace |
| **DL3056** | Pin npm global versions | warning | Pin global npm package versions |
| **DL3057** | HEALTHCHECK missing | warning | HEALTHCHECK instruction missing |
| **DL3058** | LABEL org.opencontainers | warning | Use org.opencontainers labels |
| **DL3059** | Multiple consecutive RUN | info | Multiple consecutive RUN commands |
| **DL3060** | Use pipx | info | Use pipx instead of pip for CLI tools |

---

### Maintainability (DL4xxx)

| Code | Name | Severity | Description |
|------|------|----------|-------------|
| **DL4000** | MAINTAINER deprecated | error | MAINTAINER is deprecated (use LABEL) |
| **DL4001** | wget or curl + rm | warning | Either use wget or curl but not both |
| **DL4003** | Multiple CMD instructions | warning | Multiple CMD instructions |
| **DL4004** | Multiple ENTRYPOINT | warning | Multiple ENTRYPOINT instructions |
| **DL4005** | Use SHELL instruction | warning | Use SHELL to change default shell |
| **DL4006** | Shell check pipefail | warning | Set SHELL option -o pipefail before RUN with pipe |
| **DL4007** | Use JSON for HEALTHCHECK | warning | Use JSON array for HEALTHCHECK CMD |

---

## Priority Matrix for Tally Implementation

### Critical Priority (Must Have - Phase 1)

**Security & Critical Errors:**

- DL3004 - No sudo (security)
- DL3006 - Pin base image versions (reproducibility)
- DL3020 - Use COPY instead of ADD (security)
- DL3026 - Use trusted registries (security)
- DL4000 - MAINTAINER deprecated (compliance)

**Common Mistakes:**

- DL3000 - Absolute WORKDIR
- DL3002 - Switch to non-root user
- DL3024 - FROM aliases must be unique
- DL3025 - Use JSON for CMD/ENTRYPOINT

**Estimated effort:** 1-2 weeks, ~10 rules

---

### High Priority (Should Have - Phase 2)

**Package Manager Best Practices:**

- DL3008 - Pin apt versions
- DL3009 - Clean apt cache
- DL3013 - Pin pip versions
- DL3014 - Use -y with apt-get
- DL3015 - Avoid apt-get upgrade
- DL3016 - Pin npm versions
- DL3018 - Pin apk versions
- DL3019 - Use --no-cache with apk
- DL3042 - Use --no-cache-dir with pip

**Multi-stage Builds:**

- DL3021 - COPY with --from
- DL3022 - COPY --from references valid stage
- DL3023 - COPY --from different stage
- DL3029 - No --chown with COPY --from

**Estimated effort:** 2-3 weeks, ~15 rules

---

### Medium Priority (Nice to Have - Phase 3)

**Package Managers (Other):**

- DL3028 - Pin gem versions
- DL3030-3041 - yum/dnf/zypper rules
- DL3056 - Pin npm global versions

**Best Practices:**

- DL3001 - Clean apt lists in same RUN
- DL3007 - Avoid :latest tag
- DL3010 - Use ADD for archives
- DL3043 - Avoid ONBUILD
- DL3046 - useradd without -l
- DL3057 - HEALTHCHECK missing
- DL3059 - Multiple consecutive RUN

**Maintainability:**

- DL4001 - wget OR curl, not both
- DL4003 - Multiple CMD
- DL4004 - Multiple ENTRYPOINT
- DL4005 - Use SHELL instruction
- DL4006 - Set pipefail
- DL4007 - JSON for HEALTHCHECK

**Estimated effort:** 2-3 weeks, ~20 rules

---

### Low Priority (Can Wait - Phase 4+)

**Labels & Metadata:**

- DL3048 - Invalid LABEL format
- DL3049 - LABEL deprecation
- DL3051 - LABEL schema invalid
- DL3052 - LABEL contains URL
- DL3055 - Use LABEL-schema
- DL3058 - Use org.opencontainers labels

**Advanced Optimization:**

- DL3050 - Too many COPY/ADD layers
- DL3060 - Use pipx for CLI tools

**Less Common:**

- DL3011 - Valid UNIX ports
- DL3012 - Multiple HEALTHCHECK
- DL3017 - No apk upgrade
- DL3027 - No apt-get dist-upgrade
- DL3035 - Avoid zypper dist-upgrade
- DL3044 - Forbidden ENV variables
- DL3047 - wget/curl cleanup

**Estimated effort:** 3-4 weeks, ~15 rules

---

## ShellCheck Integration

Hadolint integrates ShellCheck as a library to lint shell commands in RUN instructions.

### Most Relevant ShellCheck Rules

| Code | Name | Example |
|------|------|---------|
| **SC2046** | Unquoted command substitution | `rm $(find . -name '*.tmp')` |
| **SC2086** | Unquoted variable expansion | `rm $FILE` instead of `rm "$FILE"` |
| **SC2155** | Masking return values | `export FOO=$(command)` |
| **SC2164** | cd without error check | `cd /tmp; rm *` |

### Integration Strategy for Tally

**Phase 1:** Skip ShellCheck integration (too complex)

**Phase 2+:** Consider integration options:

1. **Subprocess approach** - Call `shellcheck` binary
2. **Library approach** - Port relevant checks to Go
3. **Recommendation** - Advise users to run shellcheck separately

---

## Configuration Examples

### Hadolint Config (.hadolint.yaml)

```yaml
# Ignore specific rules
ignored:
  - DL3006  # Don't require pinned versions
  - DL3008  # Don't require apt pinning

# Override rule severities
override:
  error:
    - DL3002  # Treat non-root warning as error
  warning:
    - DL3004  # Downgrade sudo error to warning
  info:
    - DL3015  # Downgrade upgrade warning to info
  style: []

# Trusted registries (for DL3026)
trustedRegistries:
  - docker.io
  - registry.example.com

# Inline ignore syntax
# hadolint ignore=DL3006,DL3008
# hadolint global ignore=DL3003
```

### Tally Config Equivalent (.tally.toml)

```toml
# Disable rules
[rules]
disable = ["DL3006", "DL3008"]

# Override severities
[rules.DL3002]
severity = "error"

[rules.DL3004]
severity = "warning"

# Trusted registries
[security]
trusted-registries = [
    "docker.io",
    "registry.example.com"
]
```

---

## Implementation Guidelines

### Rule Structure Template

**Note:** When implementing new rules, follow the project's established patterns:

1. **Add CLI flags** to `cmd/tally/cmd/lint.go` with `TALLY_*` environment variable sources
2. **Add rule configuration** to `internal/config/config.go` in the `RulesConfig` section
3. **Follow the max-lines pattern** from `internal/lint/rules.go` for consistent rule implementation
4. **Wire up config loading** in `loadConfigForFile()` in `lint.go`

See [CLAUDE.md](../CLAUDE.md) section "Adding New Linting Rules" for the complete checklist.

```go
// internal/rules/best_practices/pin_versions.go
package best_practices

const (
    PinVersionsCode = "DL3006"
    PinVersionsName = "Pin base image versions"
)

var PinVersionsRule = &linter.Rule{
    Code:        PinVersionsCode,
    Name:        PinVersionsName,
    Description: "Always tag the version of an image explicitly to ensure reproducible builds",
    Category:    "best-practices",
    Severity:    linter.SeverityWarning,
    URL:         "https://docs.tally.dev/rules/DL3006",
    Enabled:     true,
    Check:       checkPinVersions,
}

func init() {
    rules.Register(PinVersionsRule)
}

func checkPinVersions(ast *parser.AST, semantic *parser.SemanticModel) []linter.Violation {
    var violations []linter.Violation

    for _, stage := range semantic.Stages {
        if !hasExplicitTag(stage.BaseImage) {
            violations = append(violations, linter.Violation{
                RuleCode: PinVersionsCode,
                Message:  "Always tag the version of an image explicitly",
                Detail:   fmt.Sprintf("Image '%s' should have an explicit tag (e.g., ubuntu:22.04)", stage.BaseImage),
                File:     ast.File,
                Line:     stage.LineRange.Start,
                Severity: linter.SeverityWarning,
                DocURL:   "https://docs.tally.dev/rules/DL3006",
            })
        }
    }

    return violations
}

func hasExplicitTag(image string) bool {
    // ubuntu -> false (no tag)
    // ubuntu:latest -> true (has tag, but see DL3007)
    // ubuntu:22.04 -> true (properly pinned)
    parts := strings.Split(image, ":")
    return len(parts) > 1 && parts[1] != ""
}
```

### Testing Template

```go
func TestPinVersions(t *testing.T) {
    tests := []struct {
        name       string
        dockerfile string
        wantCount  int
    }{
        {
            name:       "untagged image",
            dockerfile: "FROM ubuntu\n",
            wantCount:  1,
        },
        {
            name:       "tagged with latest",
            dockerfile: "FROM ubuntu:latest\n",
            wantCount:  0,  // DL3007 checks for :latest
        },
        {
            name:       "properly pinned",
            dockerfile: "FROM ubuntu:22.04\n",
            wantCount:  0,
        },
        {
            name: "multi-stage mixed",
            dockerfile: `
FROM ubuntu AS builder
FROM alpine:3.18
`,
            wantCount: 1,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            violations := testutil.LintString(tt.dockerfile, PinVersionsRule)
            if len(violations) != tt.wantCount {
                t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
            }
        })
    }
}
```

---

## Quick Reference: Top 30 Rules for v1.0

1. **DL3006** - Pin base image versions
2. **DL3000** - Use absolute WORKDIR
3. **DL3002** - Last USER should not be root
4. **DL3004** - Do not use sudo
5. **DL3008** - Pin apt versions
6. **DL3009** - Delete apt-get lists
7. **DL3013** - Pin pip versions
8. **DL3014** - Use -y with apt-get
9. **DL3015** - Avoid apt-get upgrade
10. **DL3016** - Pin npm versions
11. **DL3018** - Pin apk versions
12. **DL3019** - Use --no-cache with apk
13. **DL3020** - Use COPY instead of ADD
14. **DL3021** - COPY --from usage
15. **DL3022** - COPY --from valid stage
16. **DL3024** - FROM aliases unique
17. **DL3025** - Use JSON for CMD/ENTRYPOINT
18. **DL3026** - Use trusted registries
19. **DL3042** - Use --no-cache-dir with pip
20. **DL4000** - MAINTAINER deprecated
21. **DL4001** - wget OR curl (not both)
22. **DL4003** - Multiple CMD instructions
23. **DL4004** - Multiple ENTRYPOINT
24. **DL4006** - Set pipefail before pipe
25. **DL3007** - Avoid :latest tag
26. **DL3003** - Use WORKDIR to change directory
27. **DL3001** - Delete lists in same RUN
28. **DL3046** - useradd without -l
29. **DL3057** - HEALTHCHECK missing
30. **DL3059** - Multiple consecutive RUN

---

## References

- Hadolint repository: <https://github.com/hadolint/hadolint>
- Hadolint rules: <https://github.com/hadolint/hadolint/wiki>
- ShellCheck: <https://www.shellcheck.net/>
- Docker best practices: <https://docs.docker.com/develop/develop-images/dockerfile_best-practices/>
