# BuildKit `instructions.GetMounts()` Behavior Documentation

## Summary

BuildKit's `instructions.GetMounts()` uses **deferred evaluation** - it returns default values until `RunCommand.Expand()` is called with an expander.
This is intentional design, not a bug.

## Solution

We created the `internal/runmount` package that provides a proper API for working with mounts in static analysis:

```go
import "github.com/tinovyatkin/tally/internal/runmount"

// Get properly parsed mounts from a RUN command
mounts := runmount.GetMounts(run)

// Compare mounts between RUN commands (order-independent)
if runmount.MountsEqual(mounts1, mounts2) {
    // mounts are identical
}

// Format mounts for output
formatted := runmount.FormatMounts(mounts)
// Returns: "--mount=type=cache,target=/var/cache/apt --mount=type=cache,target=/root/.cache"
```

### How It Works

The `runmount.GetMounts()` function calls `RunCommand.Expand()` with an identity expander:

```go
func identityExpander(word string) (string, error) {
    return word, nil
}

func GetMounts(run *instructions.RunCommand) []*instructions.Mount {
    // Trigger mount parsing with identity expander
    _ = run.Expand(identityExpander)
    return instructions.GetMounts(run)
}
```

This triggers the mount parsing code while preserving any ARG/ENV variables as literal strings (which is correct for static analysis).

## Root Cause Analysis

### Why GetMounts() Returns Default Values

1. **Initial parsing** (`instructions.Parse()`):
   - Calls `runMountPostHook(cmd, nil)` with **`nil` expander**

2. **Mount parsing in `parseMount()`** when `expander == nil`:

   ```go
   if expander != nil {
       // process value
   } else {
       continue  // SKIPS PARSING - deferred evaluation
   }
   ```

3. **Build-time expansion** (`dockerfile2llb`):
   - `RunCommand.Expand(expander)` is called with a real expander
   - NOW `GetMounts()` returns correct values

### Why This Design?

Mount options can contain ARG/ENV variables:

```dockerfile
ARG CACHE_DIR=/var/cache
RUN --mount=type=cache,target=$CACHE_DIR apt-get update
```

These variables can only be resolved during the actual build phase when the build context is available.

## Package API Reference

### `runmount.GetMounts(run *RunCommand) []*Mount`

Returns parsed mount configurations. Calls `Expand()` with identity expander if needed.

### `runmount.MountsEqual(a, b []*Mount) bool`

Compares two mount slices for semantic equality. Order-independent.

### `runmount.FormatMount(m *Mount) string`

Formats a single mount for output (e.g., `--mount=type=cache,target=/var/cache/apt`).

### `runmount.FormatMounts(mounts []*Mount) string`

Formats multiple mounts for a RUN instruction.

## References

- Source: [moby/buildkit/frontend/dockerfile/instructions/commands_runmount.go](https://github.com/moby/buildkit/blob/master/frontend/dockerfile/instructions/commands_runmount.go)
- Mount expansion: [moby/buildkit/frontend/dockerfile/instructions/commands.go#RunCommand.Expand](https://github.com/moby/buildkit/blob/master/frontend/dockerfile/instructions/commands.go)

---

*Document created: 2025-02-03*
*BuildKit version analyzed: v0.19.x*
