// oxlint-disable-next-line no-control-regex
const INVALID_CHARS = /[\0\r\n]/;

// Conservative shell metacharacters. We avoid these even when we don't use a shell,
// because Windows `.cmd` execution may require `cmd.exe` and quoting.
const SHELL_META = /[&|;<>`]/;

export type PathValidationResult =
  | { ok: true }
  | {
      ok: false;
      reason: string;
    };

export function validateUserSuppliedPath(value: string): PathValidationResult {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return { ok: false, reason: "empty path" };
  }
  if (INVALID_CHARS.test(trimmed)) {
    return { ok: false, reason: "contains invalid characters" };
  }
  if (SHELL_META.test(trimmed)) {
    return { ok: false, reason: "contains shell metacharacters" };
  }
  if (trimmed.includes("*") || trimmed.includes("?")) {
    return { ok: false, reason: "contains wildcard characters" };
  }
  // Reject obvious traversal patterns in user-provided values. We allow normal
  // absolute paths, but avoid resolving `..` segments from config.
  if (trimmed.split(/[\\/]+/).some((seg) => seg === "..")) {
    return { ok: false, reason: "contains traversal segments (..)" };
  }
  return { ok: true };
}
