package ruby

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/wharflab/tally/internal/testpath"
)

// fakeReader is a minimal in-memory ContextFileReader.
type fakeReader struct {
	files     map[string][]byte
	errs      map[string]error
	ignored   map[string]bool
	ignoreErr map[string]error
	reads     atomic.Int32
}

func (r *fakeReader) FileExists(path string) bool {
	if r == nil {
		return false
	}
	if _, ok := r.errs[path]; ok {
		return true
	}
	_, ok := r.files[path]
	return ok
}

func (r *fakeReader) ReadFile(path string) ([]byte, error) {
	if r == nil {
		return nil, os.ErrNotExist
	}
	r.reads.Add(1)
	if err, ok := r.errs[path]; ok {
		return nil, err
	}
	if data, ok := r.files[path]; ok {
		return append([]byte(nil), data...), nil
	}
	return nil, os.ErrNotExist
}

func (r *fakeReader) IsIgnored(path string) (bool, error) {
	if r == nil {
		return false, nil
	}
	if err, ok := r.ignoreErr[path]; ok {
		return false, err
	}
	return r.ignored[path], nil
}

func TestLoad_NilReader(t *testing.T) {
	t.Parallel()

	got := Load(nil)
	if got == nil {
		t.Fatal("Load(nil) = nil, want non-nil RubyFacts")
	}
	if got.Lockfile != nil || got.Gemfile != nil || got.RubyVersion != "" {
		t.Errorf("Load(nil) populated fields: %+v", got)
	}
}

func TestLoad_AllFilesPresent(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			gemfileLockPath:  mustReadTestdata(t, "gemfile.lock.basic"),
			gemfilePath:      mustReadTestdata(t, "Gemfile.basic"),
			rubyVersionPath:  mustReadTestdata(t, "ruby-version.basic"),
			toolVersionsPath: mustReadTestdata(t, "tool-versions.basic"),
		},
	}

	facts := Load(reader)

	if facts.Lockfile == nil {
		t.Fatal("Lockfile = nil, want non-nil")
	}
	if got, want := facts.Lockfile.BundledWith, "2.5.6"; got != want {
		t.Errorf("Lockfile.BundledWith = %q, want %q", got, want)
	}
	if facts.Gemfile == nil {
		t.Fatal("Gemfile = nil, want non-nil")
	}
	if got, want := facts.Gemfile.RubyConstraint, "3.3.5"; got != want {
		t.Errorf("Gemfile.RubyConstraint = %q, want %q", got, want)
	}
	// Priority: .ruby-version > .tool-versions > Lockfile.RubyVersion.
	if got, want := facts.RubyVersion, "3.3.5"; got != want {
		t.Errorf("RubyVersion = %q, want %q", got, want)
	}
}

func TestLoad_RubyVersionPriority(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		files map[string][]byte
		want  string
	}{
		"ruby_version_only": {
			files: map[string][]byte{
				rubyVersionPath: []byte("3.3.5\n"),
			},
			want: "3.3.5",
		},
		"tool_versions_only": {
			files: map[string][]byte{
				toolVersionsPath: []byte("ruby 3.4.0\n"),
			},
			want: "3.4.0",
		},
		"lockfile_only": {
			files: map[string][]byte{
				gemfileLockPath: mustReadTestdata(t, "gemfile.lock.basic"),
			},
			want: "3.3.5p100",
		},
		"ruby_version_beats_tool_versions": {
			files: map[string][]byte{
				rubyVersionPath:  []byte("3.3.5\n"),
				toolVersionsPath: []byte("ruby 3.4.0\n"),
			},
			want: "3.3.5",
		},
		"tool_versions_beats_lockfile": {
			files: map[string][]byte{
				toolVersionsPath: []byte("ruby 3.4.0\n"),
				gemfileLockPath:  mustReadTestdata(t, "gemfile.lock.basic"),
			},
			want: "3.4.0",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &fakeReader{files: tc.files}
			got := Load(reader).RubyVersion
			if got != tc.want {
				t.Errorf("RubyVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoad_MissingFiles(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{files: map[string][]byte{}}
	facts := Load(reader)

	if facts.Lockfile != nil {
		t.Errorf("Lockfile = %+v, want nil", facts.Lockfile)
	}
	if facts.Gemfile != nil {
		t.Errorf("Gemfile = %+v, want nil", facts.Gemfile)
	}
	if facts.RubyVersion != "" {
		t.Errorf("RubyVersion = %q, want empty", facts.RubyVersion)
	}
}

func TestLoad_MalformedFiles(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			gemfileLockPath:  []byte("hello world\n"),
			gemfilePath:      []byte("# only comments\n"),
			rubyVersionPath:  []byte("\n\n\n"),
			toolVersionsPath: []byte("nodejs 22.5.1\n"),
		},
	}
	facts := Load(reader)

	if facts.Lockfile != nil {
		t.Errorf("Lockfile = %+v, want nil for malformed input", facts.Lockfile)
	}
	if facts.Gemfile != nil {
		t.Errorf("Gemfile = %+v, want nil for comment-only input", facts.Gemfile)
	}
	if facts.RubyVersion != "" {
		t.Errorf("RubyVersion = %q, want empty when no ruby is observable", facts.RubyVersion)
	}
}

func TestLoad_ReadErrorsTreatedAsAbsent(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{},
		errs: map[string]error{
			gemfileLockPath:  errors.New("permission denied"),
			gemfilePath:      errors.New("io error"),
			rubyVersionPath:  errors.New("io error"),
			toolVersionsPath: errors.New("io error"),
		},
	}
	facts := Load(reader)
	if facts.Lockfile != nil || facts.Gemfile != nil || facts.RubyVersion != "" {
		t.Errorf("Load with errors populated fields: %+v", facts)
	}
}

func TestLoad_EncryptedCredentials_NotPresent(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{files: map[string][]byte{}}
	facts := Load(reader)

	if facts.HasEncryptedCredentials {
		t.Errorf("HasEncryptedCredentials = true, want false")
	}
	if len(facts.EncryptedCredentialsPaths) != 0 {
		t.Errorf("EncryptedCredentialsPaths = %v, want empty", facts.EncryptedCredentialsPaths)
	}
}

func TestLoad_EncryptedCredentials_RootFile(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			credentialsEncFilePath: []byte("encrypted-blob"),
		},
	}
	facts := Load(reader)

	if !facts.HasEncryptedCredentials {
		t.Errorf("HasEncryptedCredentials = false, want true")
	}
	if got, want := len(facts.EncryptedCredentialsPaths), 1; got != want {
		t.Fatalf("len(EncryptedCredentialsPaths) = %d, want %d", got, want)
	}
	if facts.EncryptedCredentialsPaths[0] != credentialsEncFilePath {
		t.Errorf("EncryptedCredentialsPaths[0] = %q, want %q",
			facts.EncryptedCredentialsPaths[0], credentialsEncFilePath)
	}
}

func TestLoad_EncryptedCredentials_PerEnvironment(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			"config/credentials/production.yml.enc": []byte("encrypted-blob"),
			"config/credentials/staging.yml.enc":    []byte("encrypted-blob"),
		},
	}
	facts := Load(reader)

	if !facts.HasEncryptedCredentials {
		t.Errorf("HasEncryptedCredentials = false, want true")
	}
	if len(facts.EncryptedCredentialsPaths) != 2 {
		t.Errorf("EncryptedCredentialsPaths = %v, want 2 entries",
			facts.EncryptedCredentialsPaths)
	}
}

func TestLoad_EncryptedCredentials_DockerignoredSkipped(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			credentialsEncFilePath: []byte("encrypted-blob"),
		},
		ignored: map[string]bool{
			credentialsEncFilePath: true,
		},
	}
	facts := Load(reader)
	if facts.HasEncryptedCredentials {
		t.Errorf("HasEncryptedCredentials = true, want false (file is .dockerignored)")
	}
}

func TestExtractRubyVersionFromLockfile(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		"with_ruby_prefix": {"ruby 3.3.5p100", "3.3.5p100"},
		"capitalized":      {"Ruby 3.3.5", "3.3.5"},
		"plain":            {"3.3.5", "3.3.5"},
		"empty":            {"", ""},
		"single_word":      {"jruby", "jruby"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := extractRubyVersionFromLockfile(tc.in); got != tc.want {
				t.Errorf("extractRubyVersionFromLockfile(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLoad_DockerignoredFilesAreSkipped(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			gemfileLockPath: mustReadTestdata(t, "gemfile.lock.basic"),
			gemfilePath:     mustReadTestdata(t, "Gemfile.basic"),
			rubyVersionPath: []byte("3.3.5\n"),
		},
		ignored: map[string]bool{
			gemfileLockPath: true,
			gemfilePath:     true,
			rubyVersionPath: true,
		},
	}

	facts := Load(reader)
	if facts.Lockfile != nil {
		t.Errorf("Lockfile = %+v, want nil for ignored path", facts.Lockfile)
	}
	if facts.Gemfile != nil {
		t.Errorf("Gemfile = %+v, want nil for ignored path", facts.Gemfile)
	}
	if facts.RubyVersion != "" {
		t.Errorf("RubyVersion = %q, want empty when .ruby-version is ignored", facts.RubyVersion)
	}
	if reader.reads.Load() != 0 {
		t.Errorf("ReadFile called %d times for ignored paths; want 0", reader.reads.Load())
	}
}

func TestLoad_IgnoredCheckErrorTreatedAsIgnored(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		files: map[string][]byte{
			gemfileLockPath: mustReadTestdata(t, "gemfile.lock.basic"),
		},
		ignoreErr: map[string]error{
			gemfileLockPath: errors.New("dockerignore unreadable"),
		},
	}

	facts := Load(reader)
	if facts.Lockfile != nil {
		t.Errorf("Lockfile = %+v, want nil when IsIgnored returns an error", facts.Lockfile)
	}
}

func mustReadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := testpath.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read testdata %q: %v", name, err)
	}
	return data
}
