.PHONY: build check-shellcheck-wasm intellij-plugin intellij-plugin-verify intellij-plugin-smoke test test-verbose lint lint-fix deadcode cpd clean release publish-prepare publish-gem publish jsonschema schema-gen schema-check lsp-protocol print-gotestsum-bin shellcheck-wasm update-shellcheck-wasm

GOEXPERIMENT ?= jsonv2
export GOEXPERIMENT

# Build tags for the shipped full-featured build (keep CGO enabled while
# disabling containers/image transports we do not ship).
BUILDTAGS := containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub
INTELLIJ_PLUGIN_VERSION := $(shell sed -n 's/^plugin_version = "\(.*\)"/\1/p' _integrations/intellij-tally/build/versions.toml | head -n 1)

build: check-shellcheck-wasm
	TALLY_VERSION="$${TALLY_VERSION:-0.0.0-dev}"; \
	bazel build --config=release --embed_label="$$TALLY_VERSION" //:tally; \
	src="$$(tools/bazel/target_output.sh --config=release --embed_label="$$TALLY_VERSION" //:tally)"; \
	dst=tally; \
	case "$$src" in *.exe) dst=tally.exe ;; esac; \
	cp "$$src" "$$dst"

# Friendlier diagnostic than the raw `go:embed` error when the wasm artifact is
# missing. We don't auto-build here because that would silently pull Docker on
# an ordinary `make build`.
.PHONY: check-shellcheck-wasm
check-shellcheck-wasm:
	@if [ ! -s internal/shellcheck/wasm/shellcheck.wasm ]; then \
		echo "internal/shellcheck/wasm/shellcheck.wasm is missing."; \
		echo "Run 'make shellcheck-wasm' (Bazel-backed, requires Docker) to build it, or"; \
		echo "download the artifact from a recent CI run."; \
		exit 1; \
	fi

intellij-plugin:
	bazel build --//:release_version="$(INTELLIJ_PLUGIN_VERSION)" //_integrations/intellij-tally:plugin_zip
	mkdir -p _integrations/intellij-tally/dist
	install -m 0644 bazel-bin/_integrations/intellij-tally/tally-intellij-plugin.zip \
		_integrations/intellij-tally/dist/tally-intellij-plugin-$(INTELLIJ_PLUGIN_VERSION).zip

intellij-plugin-verify: intellij-plugin
	bash _integrations/intellij-tally/build/smoke.sh

intellij-plugin-smoke:
	bash _integrations/intellij-tally/build/smoke.sh

GOTESTSUM_VERSION := v1.13.0
GOLANGCI_LINT_VERSION := $(shell cat .golangci-lint-version | tr -d '[:space:]')
DEADCODE_VERSION := v0.41.0

test: check-shellcheck-wasm
	bazel test --config=go --config=race //cmd/... //internal/... //_tools/...

test-verbose: check-shellcheck-wasm
	bazel test --config=go --config=race --test_output=all //cmd/... //internal/... //_tools/...

lint: check-shellcheck-wasm bin/golangci-lint-$(GOLANGCI_LINT_VERSION) bin/custom-gcl
	bin/custom-gcl run

bin/custom-gcl: bin/golangci-lint-$(GOLANGCI_LINT_VERSION) .custom-gcl.yml _tools/customlint/*.go
	bin/golangci-lint custom

lint-fix: check-shellcheck-wasm bin/golangci-lint-$(GOLANGCI_LINT_VERSION) bin/custom-gcl
	bin/custom-gcl run --fix

# Filter out internal/lsp/protocol/ from deadcode: that package is generated
# and only a subset of LSP methods are dispatched, so helpers backing unused
# methods are expected to appear unreachable.
deadcode: check-shellcheck-wasm bin/deadcode-$(DEADCODE_VERSION)
	@tmp=$$(mktemp); \
	bin/deadcode -test ./... 2>&1 | grep -v 'internal/lsp/protocol/' >"$$tmp" || true; \
	if [ -s "$$tmp" ]; then cat "$$tmp"; rm "$$tmp"; exit 1; fi; \
	rm "$$tmp"

PMD_VERSION := 7.20.0

cpd: bin/pmd-$(PMD_VERSION)
	@find . -type f \( \
		-name "*_test.go" \
		-o -name "*.pb.go" \
		-o -name "*_generated.go" \
		-o -name "*.gen.go" \
		-o -name "*_gen.go" \
		-o -path "*/testdata/*" \
		-o -path "*/__snapshots__/*" \
		-o -path "*/packaging/*" \
		-o -path "*/bin/*" \
	\) > .cpd-exclude.txt
	@bin/pmd-bin-$(PMD_VERSION)/bin/pmd cpd --language go --minimum-tokens 100 \
		--dir . --exclude-file-list .cpd-exclude.txt --format markdown
	@rm -f .cpd-exclude.txt

bin/pmd-$(PMD_VERSION):
	@mkdir -p bin
	@if [ ! -d "bin/pmd-bin-$(PMD_VERSION)" ]; then \
		curl -fL "https://github.com/pmd/pmd/releases/download/pmd_releases%2F$(PMD_VERSION)/pmd-dist-$(PMD_VERSION)-bin.zip" -o bin/pmd.zip; \
		cd bin && unzip -q pmd.zip && rm pmd.zip; \
	fi
	@touch $@

bin/golangci-lint-$(GOLANGCI_LINT_VERSION):
	@rm -f bin/golangci-lint bin/golangci-lint-*
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b bin/ v$(GOLANGCI_LINT_VERSION)
	@touch $@

bin/deadcode-$(DEADCODE_VERSION):
	@rm -f bin/deadcode bin/deadcode-*
	GOBIN=$(CURDIR)/bin go install golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION)
	@touch $@

bin/gotestsum-$(GOTESTSUM_VERSION):
	@rm -f bin/gotestsum bin/gotestsum-*
	@mkdir -p bin
	GOBIN=$(CURDIR)/bin go install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)
	@mv bin/gotestsum bin/gotestsum-$(GOTESTSUM_VERSION)
	@touch bin/gotestsum-$(GOTESTSUM_VERSION)

print-gotestsum-bin:
	@echo bin/gotestsum-$(GOTESTSUM_VERSION)

# Schema pipeline:
# - schema-gen: regenerate all JSON schemas, generated Go schema models, and published schema.json.
# - schema-check: enforce schema-generation invariants used by CI.
# - jsonschema: compatibility alias for schema-check.
schema-gen:
	cd _tools && go run ./schema-gen

schema-check: schema-gen
	# Namespace index schemas are generated artifacts; fail if regeneration causes drift.
	@if test -n "$$(git status --porcelain -- internal/rules/*/index.schema.json)"; then \
		echo "namespace index schema drift detected; run make schema-gen and commit index.schema.json changes"; \
		git --no-pager diff -- internal/rules/*/index.schema.json; \
		git status --short -- internal/rules/*/index.schema.json; \
		exit 1; \
	fi
	# Generated schema models must stay JSON v2 compliant (no encoding/json imports).
	@if rg -n '"encoding/json"' internal/schemas/generated; then \
		echo "generated schema models must not import encoding/json"; \
		exit 1; \
	fi
	# Published schema.json must be standalone for SchemaStore/IDE consumers.
	@if rg -n '"\\$$ref"[[:space:]]*:[[:space:]]*"(\\./|\\.\\./)' schema.json; then \
		echo "published schema.json must not contain filesystem-relative $$ref paths"; \
		exit 1; \
	fi

jsonschema: schema-check

lsp-protocol:
	bun run _tools/lspgen/fetchModel.mts
	bun run _tools/lspgen/generate.mts

# File target: the embedded ShellCheck wasm. Bazel owns the actual build and
# tracks the pins, Dockerfile, Reactor, and ast-grep rewrite inputs. The
# artifact is .gitignored; this target materializes Bazel's declared output
# into the source location needed by raw `go build`/`go test` tools.
SHELLCHECK_WASM := internal/shellcheck/wasm/shellcheck.wasm

shellcheck-wasm:
	@mkdir -p $(dir $(SHELLCHECK_WASM))
	bazel build //_tools/shellcheck-wasm:shellcheck_wasm
	cp bazel-bin/_tools/shellcheck-wasm/shellcheck.wasm $(SHELLCHECK_WASM)
	@touch $(SHELLCHECK_WASM)

$(SHELLCHECK_WASM): shellcheck-wasm ;

# Force-rebuild target kept for humans who want to refresh after pulling new
# upstream ShellCheck source without touching a pinned version.
update-shellcheck-wasm:
	rm -f $(SHELLCHECK_WASM)
	$(MAKE) shellcheck-wasm

clean:
	rm -f tally
	rm -rf bin/ dist/
	# Note: $(SHELLCHECK_WASM) is not removed here because rebuilding it
	# requires Docker and several minutes. Use `make update-shellcheck-wasm`
	# to force-refresh, or delete the file manually.

# Release and publish targets
# Prerequisites:
#   - NPM_API_KEY env var (or npm login)
#   - UV_PUBLISH_TOKEN env var for PyPI
#   - ~/.gem/credentials for RubyGems

release:
	@echo "release is now orchestrated by .github/workflows/release.yml on native GitHub runners."
	@echo "Local multi-platform release from the Makefile has been removed."
	@exit 1

publish-prepare: release
	cd packaging/rubygems && rake prepare

publish-gem: publish-prepare
	cd packaging/rubygems && rake publish

publish: publish-gem
