package shell

import "testing"

func TestFirstGitSourceOpportunity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		script            string
		workdir           string
		wantSource        string
		wantChecksum      string
		wantDestination   string
		wantPreceding     string
		wantRemaining     string
		wantKeepGitDir    bool
		wantUsesSubmodule bool
	}{
		{
			name:            "branch ref keeps variable text unescaped",
			script:          `git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}`,
			workdir:         "/src",
			wantSource:      `https://github.com/aws/aws-ofi-nccl.git?ref=v${BRANCH_OFI}`,
			wantDestination: `/src/aws-ofi-nccl`,
		},
		{
			name: "extracts middle clone and full checkout commit",
			script: "echo foo && git clone https://github.com/NVIDIA/apex && " +
				"cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo zoo",
			workdir:         "/",
			wantSource:      `https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567`,
			wantChecksum:    `0123456789abcdef0123456789abcdef01234567`,
			wantDestination: `/apex`,
			wantPreceding:   `echo foo`,
			wantRemaining:   `cd /apex && echo zoo`,
		},
		{
			name:            "gitlab http remotes keep ref query for variable commits",
			script:          `git clone https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git -b ${GHC_WASM_META_COMMIT}`,
			workdir:         "/ghc",
			wantSource:      `https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git?ref=${GHC_WASM_META_COMMIT}`,
			wantDestination: `/ghc/ghc-wasm-meta`,
		},
		{
			name:            "keeps git dir when remaining commands still use git",
			script:          `git clone https://github.com/NVIDIA/apex && cd apex && git describe --tags`,
			workdir:         "/work",
			wantSource:      `https://github.com/NVIDIA/apex.git`,
			wantDestination: `/work/apex`,
			wantRemaining:   `cd /work/apex && git describe --tags`,
			wantKeepGitDir:  true,
		},
		{
			name: "preserves explicit destination and submodules",
			script: "cd /tmp && git clone --recurse-submodules " +
				"https://github.com/example/project.git src/project && cd src/project && make",
			workdir:           "/",
			wantSource:        `https://github.com/example/project.git?submodules=true`,
			wantDestination:   `/tmp/src/project`,
			wantPreceding:     `cd /tmp`,
			wantRemaining:     `cd /tmp/src/project && make`,
			wantUsesSubmodule: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := FirstGitSourceOpportunity(tt.script, VariantBash, tt.workdir)
			if !ok {
				t.Fatal("FirstGitSourceOpportunity() returned ok=false")
			}
			if got.AddSource != tt.wantSource {
				t.Fatalf("AddSource = %q, want %q", got.AddSource, tt.wantSource)
			}
			if got.AddDestination != tt.wantDestination {
				t.Fatalf("AddDestination = %q, want %q", got.AddDestination, tt.wantDestination)
			}
			if got.AddChecksum != tt.wantChecksum {
				t.Fatalf("AddChecksum = %q, want %q", got.AddChecksum, tt.wantChecksum)
			}
			if got.PrecedingCommands != tt.wantPreceding {
				t.Fatalf("PrecedingCommands = %q, want %q", got.PrecedingCommands, tt.wantPreceding)
			}
			if got.RemainingCommands != tt.wantRemaining {
				t.Fatalf("RemainingCommands = %q, want %q", got.RemainingCommands, tt.wantRemaining)
			}
			if got.KeepGitDir != tt.wantKeepGitDir {
				t.Fatalf("KeepGitDir = %v, want %v", got.KeepGitDir, tt.wantKeepGitDir)
			}
			if got.UsesSubmodules != tt.wantUsesSubmodule {
				t.Fatalf("UsesSubmodules = %v, want %v", got.UsesSubmodules, tt.wantUsesSubmodule)
			}
		})
	}
}

func TestFirstGitSourceOpportunity_RejectsComplexScripts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "subshell",
			script: `(git clone https://github.com/NVIDIA/apex)`,
		},
		{
			name:   "unsupported clone flag",
			script: `git clone --filter=blob:none https://github.com/NVIDIA/apex`,
		},
		{
			name:   "abbreviated checkout commit",
			script: `git clone https://github.com/NVIDIA/apex && cd apex && git checkout aa756ce`,
		},
		{
			name:   "dynamic destination path",
			script: `git clone https://github.com/NVIDIA/apex ${TARGET_DIR}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, ok := FirstGitSourceOpportunity(tt.script, VariantBash, "/"); ok {
				t.Fatalf("FirstGitSourceOpportunity() = %+v, want ok=false", got)
			}
		})
	}
}
