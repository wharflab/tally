package ruby

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeReader is a minimal in-memory ContextFileReader.
type fakeReader struct {
	files map[string][]byte
	errs  map[string]error
	reads atomic.Int32
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

func TestLoadCached_SameReaderSameResult(t *testing.T) {
	t.Parallel()
	// Each test creates a fresh fakeReader, so the global cache entry never
	// collides with concurrent tests; no global reset is required.
	reader := &fakeReader{
		files: map[string][]byte{
			gemfileLockPath: mustReadTestdata(t, "gemfile.lock.basic"),
		},
	}

	first := LoadCached(reader)
	second := LoadCached(reader)

	if first != second {
		t.Errorf("LoadCached returned different pointers: %p vs %p", first, second)
	}
	// First call reads Gemfile.lock; the second call must not re-read.
	if reader.reads.Load() == 0 {
		t.Errorf("expected at least one read on first call")
	}
	readsAfterFirst := reader.reads.Load()
	_ = LoadCached(reader)
	if got := reader.reads.Load(); got != readsAfterFirst {
		t.Errorf("LoadCached re-read after cache hit: %d > %d", got, readsAfterFirst)
	}
}

func TestLoadCached_NilReader(t *testing.T) {
	t.Parallel()

	facts := LoadCached(nil)
	if facts == nil {
		t.Fatal("LoadCached(nil) = nil, want non-nil")
	}
	if facts.Lockfile != nil || facts.Gemfile != nil {
		t.Errorf("LoadCached(nil) returned populated facts: %+v", facts)
	}
}

func mustReadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %q: %v", name, err)
	}
	return data
}
