package shell

import (
	"net/url"
	"path"
	"slices"
	"strings"
)

const (
	invokeWebRequestCommand = "invoke-webrequest"
	iwrCommand              = "iwr"
)

// ArchiveExtensions is the unified superset of archive file extensions
// recognized by both DL3010 (hadolint) and prefer-add-unpack (tally).
// Sorted longest-first so suffix matching is greedy
// (e.g. ".tar.gz" is checked before ".gz").
var ArchiveExtensions = []string{
	".tar.lzma",
	".tar.bz2",
	".tar.gz",
	".tar.xz",
	".tar.zst",
	".tar.lz",
	".tar.Z",
	".lzma",
	".tbz2",
	".tzst",
	".tar",
	".tbz",
	".tb2",
	".tgz",
	".tlz",
	".tpz",
	".txz",
	".bz2",
	".tZ",
	".gz",
	".lz",
	".xz",
	".Z",
}

// DownloadCommands lists commands that download remote files.
var DownloadCommands = []string{"curl", "wget", invokeWebRequestCommand, iwrCommand}

// ExtractionCommands lists commands that extract archive files
// (excluding tar, which needs separate flag checking via IsTarExtract).
var ExtractionCommands = []string{
	"bunzip2",
	"gzcat",
	"gunzip",
	"uncompress",
	"unlzma",
	"unxz",
	"unzip",
	"zcat",
	"zgz",
}

// IsArchiveFilename checks if a filename has a recognized archive extension.
// Extensions are case-sensitive (e.g. .Z and .tZ use uppercase Z for Unix
// compress format).
func IsArchiveFilename(name string) bool {
	return slices.ContainsFunc(ArchiveExtensions, func(ext string) bool {
		return strings.HasSuffix(name, ext)
	})
}

// IsURL checks if a string is a valid URL with an http, https, or ftp scheme.
func IsURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https", "ftp":
		return u.Host != ""
	default:
		return false
	}
}

// IsArchiveURL checks if a URL string points to an archive file.
// Strips query/fragment before checking extension. Requires http/https/ftp scheme.
func IsArchiveURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https", "ftp":
		return u.Host != "" && IsArchiveFilename(path.Base(u.Path))
	default:
		return false
	}
}

// IsTarExtract checks if a tar CommandInfo has extraction flags
// (-x, --extract, --get).
func IsTarExtract(cmd *CommandInfo) bool {
	for _, arg := range cmd.Args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		// Long flags
		if arg == "--extract" || arg == "--get" {
			return true
		}
		// Short flags: any flag starting with - (but not --) that contains 'x'
		if !strings.HasPrefix(arg, "--") && strings.Contains(arg, "x") {
			return true
		}
	}
	return false
}

// TarDestination extracts the target directory from a tar CommandInfo.
// Checks -C <dir>, --directory=<dir>, --directory <dir>. Returns "" if none found.
func TarDestination(cmd *CommandInfo) string {
	for i, arg := range cmd.Args {
		// --directory=<value>
		if after, found := strings.CutPrefix(arg, "--directory="); found {
			return after
		}
		// --directory <value>
		if arg == "--directory" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
		// -C <value> (short flag — must not be a long flag)
		if arg == "-C" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}
	return ""
}

// DropQuotes removes surrounding single or double quotes from a string.
func DropQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// DownloadOutputFile extracts the output filename from a curl or wget CommandInfo.
// For curl: -o <file>, -o<file>, --output <file>, --output=<file>
// For wget: -O <file>, -O<file>, --output-document <file>, --output-document=<file>
// Returns "" if no output file is specified or if output is stdout ("-").
func DownloadOutputFile(cmd *CommandInfo) string {
	var short, long string
	switch cmd.Name {
	case "curl":
		short, long = "-o", "--output"
	case "wget":
		short, long = "-O", "--output-document"
	case invokeWebRequestCommand, iwrCommand:
		return cmd.GetArgValue("-OutFile")
	default:
		return ""
	}
	for i, arg := range cmd.Args {
		// --output=<file> / --output-document=<file>
		if after, found := strings.CutPrefix(arg, long+"="); found {
			if after == "-" {
				return ""
			}
			return after
		}
		// Attached short form: -o<file> / -O<file>
		if after, found := strings.CutPrefix(arg, short); found && after != "" {
			if after == "-" {
				return ""
			}
			return after
		}
		// Spaced form: -o <file> / --output <file>
		if (arg == short || arg == long) && i+1 < len(cmd.Args) {
			val := cmd.Args[i+1]
			if val == "-" {
				return ""
			}
			return val
		}
	}
	return ""
}

// DownloadURL extracts the first URL argument (http/https/ftp) from a download CommandInfo.
// Returns "" if no URL is found.
func DownloadURL(cmd *CommandInfo) string {
	switch cmd.Name {
	case invokeWebRequestCommand, iwrCommand:
		if uri := DropQuotes(cmd.GetArgValue("-Uri")); uri != "" && IsURL(uri) {
			return uri
		}
	}
	if i := slices.IndexFunc(cmd.Args, func(arg string) bool { return IsURL(DropQuotes(arg)) }); i >= 0 {
		return DropQuotes(cmd.Args[i])
	}
	return ""
}

// Basename extracts the filename from a path, stripping quotes and handling
// both Unix and Windows separators.
func Basename(p string) string {
	p = DropQuotes(p)
	// Handle Windows backslash paths
	if i := strings.LastIndexByte(p, '\\'); i >= 0 {
		p = p[i+1:]
	}
	return path.Base(p)
}
