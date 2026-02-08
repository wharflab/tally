// Package runmount provides utilities for working with RUN --mount options.
//
// BuildKit's instructions.GetMounts() uses deferred evaluation - it returns
// default values until RunCommand.Expand() is called with an expander.
// This package provides helpers to eagerly parse mount options for static analysis.
package runmount

import (
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
)

// identityExpander returns input unchanged, enabling mount parsing without variable expansion.
func identityExpander(word string) (string, error) {
	return word, nil
}

// GetMounts returns parsed mount configurations from a RUN command.
// Unlike instructions.GetMounts(), this eagerly parses mount options
// by calling Expand() with an identity expander if needed.
//
// This is safe for static analysis - any ARG/ENV variables in mount
// options will be preserved as literal strings.
func GetMounts(run *instructions.RunCommand) []*instructions.Mount {
	// Check if mounts are already populated
	mounts := instructions.GetMounts(run)
	if len(mounts) > 0 && mountsPopulated(mounts) {
		return mounts
	}

	// Check if there are any mount flags to parse
	if !hasMountFlags(run) {
		return nil
	}

	// Trigger mount parsing with identity expander
	// This populates the mount state with actual values
	_ = run.Expand(identityExpander) //nolint:errcheck // identity expander never fails

	return instructions.GetMounts(run)
}

// hasMountFlags checks if the RUN command has any mount flags.
func hasMountFlags(run *instructions.RunCommand) bool {
	return slices.ContainsFunc(run.FlagsUsed, func(flag string) bool {
		return strings.HasPrefix(flag, "mount")
	})
}

// mountsPopulated checks if mounts have been properly parsed (not just defaults).
// Default unparsed mounts have Type=bind and empty Target.
func mountsPopulated(mounts []*instructions.Mount) bool {
	for _, m := range mounts {
		// A properly parsed mount should have a target (except for secret/ssh which use ID)
		if m.Target != "" || m.CacheID != "" {
			return true
		}
		// If type is not bind, it was parsed
		if m.Type != instructions.MountTypeBind {
			return true
		}
	}
	return false
}

// MountsEqual compares two mount slices for semantic equality.
// Order-independent comparison of mount configurations.
func MountsEqual(a, b []*instructions.Mount) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}

	// Create maps for comparison (using mount key as identifier)
	aMap := make(map[string]*instructions.Mount, len(a))
	for _, m := range a {
		aMap[mountKey(m)] = m
	}

	for _, m := range b {
		key := mountKey(m)
		am, ok := aMap[key]
		if !ok {
			return false
		}
		if !mountEqual(am, m) {
			return false
		}
	}

	return true
}

// mountKey generates a unique key for a mount based on type and target.
func mountKey(m *instructions.Mount) string {
	// For secret/ssh mounts, use ID as key; for others use target
	if m.Type == instructions.MountTypeSecret || m.Type == instructions.MountTypeSSH {
		return string(m.Type) + ":" + m.CacheID
	}
	return string(m.Type) + ":" + m.Target
}

// mountEqual compares two mounts for equality.
func mountEqual(a, b *instructions.Mount) bool {
	if a.Type != b.Type {
		return false
	}
	if a.Target != b.Target {
		return false
	}
	if a.Source != b.Source {
		return false
	}
	if a.From != b.From {
		return false
	}
	if a.ReadOnly != b.ReadOnly {
		return false
	}
	if a.CacheID != b.CacheID {
		return false
	}
	if a.CacheSharing != b.CacheSharing {
		return false
	}
	// Compare optional fields
	if !uint64PtrEqual(a.UID, b.UID) {
		return false
	}
	if !uint64PtrEqual(a.GID, b.GID) {
		return false
	}
	if !uint64PtrEqual(a.Mode, b.Mode) {
		return false
	}
	return true
}

func uint64PtrEqual(a, b *uint64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// FormatMount formats a mount for output in a RUN instruction.
func FormatMount(m *instructions.Mount) string {
	parts := []string{"type=" + string(m.Type)}

	switch m.Type {
	case instructions.MountTypeCache:
		parts = formatCacheMount(parts, m)
	case instructions.MountTypeBind:
		parts = formatBindMount(parts, m)
	case instructions.MountTypeTmpfs:
		parts = formatTmpfsMount(parts, m)
	case instructions.MountTypeSecret, instructions.MountTypeSSH:
		parts = formatSecretSSHMount(parts, m)
	}

	return "--mount=" + joinParts(parts)
}

func formatCacheMount(parts []string, m *instructions.Mount) []string {
	if m.Target != "" {
		parts = append(parts, "target="+m.Target)
	}
	if m.CacheID != "" && m.CacheID != m.Target {
		parts = append(parts, "id="+m.CacheID)
	}
	if m.CacheSharing != "" && m.CacheSharing != instructions.MountSharingShared {
		parts = append(parts, "sharing="+string(m.CacheSharing))
	}
	if m.From != "" {
		parts = append(parts, "from="+m.From)
	}
	if m.Source != "" {
		parts = append(parts, "source="+m.Source)
	}
	if m.ReadOnly {
		parts = append(parts, "ro")
	}
	parts = appendOwnershipParts(parts, m)
	return parts
}

func formatBindMount(parts []string, m *instructions.Mount) []string {
	if m.Target != "" {
		parts = append(parts, "target="+m.Target)
	}
	if m.Source != "" {
		parts = append(parts, "source="+m.Source)
	}
	if m.From != "" {
		parts = append(parts, "from="+m.From)
	}
	if m.ReadOnly {
		parts = append(parts, "ro")
	}
	return parts
}

func formatTmpfsMount(parts []string, m *instructions.Mount) []string {
	if m.Target != "" {
		parts = append(parts, "target="+m.Target)
	}
	if m.SizeLimit > 0 {
		parts = append(parts, "size="+formatBytes(m.SizeLimit))
	}
	return parts
}

func formatSecretSSHMount(parts []string, m *instructions.Mount) []string {
	if m.CacheID != "" {
		parts = append(parts, "id="+m.CacheID)
	}
	if m.Target != "" {
		parts = append(parts, "target="+m.Target)
	}
	if m.Required {
		parts = append(parts, "required")
	}
	parts = appendOwnershipParts(parts, m)
	return parts
}

func appendOwnershipParts(parts []string, m *instructions.Mount) []string {
	if m.UID != nil {
		parts = append(parts, "uid="+formatUint64(*m.UID))
	}
	if m.GID != nil {
		parts = append(parts, "gid="+formatUint64(*m.GID))
	}
	if m.Mode != nil {
		parts = append(parts, "mode="+formatOctal(*m.Mode))
	}
	return parts
}

// FormatMounts formats multiple mounts for a RUN instruction.
func FormatMounts(mounts []*instructions.Mount) string {
	if len(mounts) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, m := range mounts {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(FormatMount(m))
	}
	return sb.String()
}

func joinParts(parts []string) string {
	return strings.Join(parts, ",")
}

func formatUint64(v uint64) string {
	return strconv.FormatUint(v, 10)
}

func formatOctal(v uint64) string {
	// Format as 4-digit octal with leading zeros
	s := strconv.FormatUint(v, 8)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func formatBytes(v int64) string {
	// Simple byte formatting - could be enhanced
	return strconv.FormatInt(v, 10)
}
