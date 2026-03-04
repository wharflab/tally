# tally/sort-packages

Package lists in install commands should be sorted alphabetically.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Enabled |
| Auto-fix | Yes (safe) |

## Description

Whenever possible, multi-line arguments should be sorted alphanumerically to make maintenance easier. This helps to avoid duplication of packages and
makes the list much easier to update. This also makes PRs a lot easier to read and review.

This rule enforces the [official Docker best practice](https://docs.docker.com/build/building/best-practices/#sort-multi-line-arguments) for sorting
package lists across common package manager install commands.

### Supported Package Managers

| Manager | Install subcommands |
|---------|-------------------|
| apt-get, apt | `install` |
| apk | `add` |
| dnf, yum | `install` |
| zypper | `install`, `in` |
| npm | `install`, `i`, `add` |
| yarn | `add` |
| pnpm | `add`, `install`, `i` |
| pip, pip3 | `install` |
| bun | `add`, `install`, `i` |
| composer | `require` |

### Sort key extraction

Version specifiers are stripped for comparison:

- `flask==2.0` sorts as `flask`
- `curl=7.88.1-10+deb12u5` sorts as `curl`
- `@eslint/js@8.0.0` sorts as `@eslint/js` (npm scoped package)

Sorting is case-insensitive.

### Variable arguments

When install commands mix literal packages and variable references (`$PKG`, `${PKG}`), only the literal packages are sorted. Variables are kept at the
end in their original relative order.

### Skipped cases

No violation is emitted when:

- Fewer than 2 literal packages (nothing to sort)
- File-based install: `pip install -r requirements.txt`, `pip install -e .`
- All arguments are variables
- Exec-form RUN: `RUN ["apt-get", "install", "curl"]`
- Heredoc RUN
- Packages are already sorted

## Examples

### Bad

```dockerfile
RUN apt-get update && apt-get install -y \
    wget \
    curl \
    git \
    mercurial \
    subversion

RUN npm install express axios
```

### Good

```dockerfile
RUN apt-get update && apt-get install -y \
    curl \
    git \
    mercurial \
    subversion \
    wget

RUN npm install axios express
```

## Auto-fix

This rule provides a safe auto-fix that sorts packages in-place. Only the package name text is replaced; whitespace, continuation backslashes, and
newlines are preserved.

```bash
tally lint --fix Dockerfile
```

## Configuration

No custom configuration options. The rule is enabled by default with severity "style".

```toml
# Disable the rule
[rules.tally.sort-packages]
severity = "off"
```

## References

- [Docker official best practices: Sort multi-line arguments](https://docs.docker.com/build/building/best-practices/#sort-multi-line-arguments)
