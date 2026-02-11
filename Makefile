.PHONY: build test test-verbose lint lint-fix deadcode cpd clean release publish-prepare publish-npm publish-pypi publish-gem publish jsonschema lsp-protocol print-gotestsum-bin

GOEXPERIMENT ?= jsonv2
export GOEXPERIMENT

build:
	GOSUMDB=sum.golang.org CGO_ENABLED=0 go build -ldflags "-s -w" -o tally

GOTESTSUM_VERSION := v1.13.0
GOLANGCI_LINT_VERSION := v2.9.0
GORELEASER_VERSION := v2.13.3
DEADCODE_VERSION := v0.41.0

test: bin/gotestsum-$(GOTESTSUM_VERSION)
	bin/gotestsum-$(GOTESTSUM_VERSION) --format testname -- -race -count=1 -timeout=30s ./...

test-verbose: bin/gotestsum-$(GOTESTSUM_VERSION)
	bin/gotestsum-$(GOTESTSUM_VERSION) --format standard-verbose -- -race -count=1 -timeout=30s ./...

lint: bin/golangci-lint-$(GOLANGCI_LINT_VERSION) bin/custom-gcl
	bin/custom-gcl run

bin/custom-gcl: bin/golangci-lint-$(GOLANGCI_LINT_VERSION) .custom-gcl.yml _tools/customlint/*.go
	bin/golangci-lint custom

lint-fix: bin/golangci-lint-$(GOLANGCI_LINT_VERSION) bin/custom-gcl
	bin/custom-gcl run --fix

# Filter out internal/lsp/protocol/ from deadcode: that package is generated
# and only a subset of LSP methods are dispatched, so helpers backing unused
# methods are expected to appear unreachable.
deadcode: bin/deadcode-$(DEADCODE_VERSION)
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
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b bin/ $(GOLANGCI_LINT_VERSION)
	@touch $@

bin/goreleaser-$(GORELEASER_VERSION):
	@rm -f bin/goreleaser bin/goreleaser-*
	GOBIN=$(CURDIR)/bin go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
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

jsonschema:
	go run gen/jsonschema.go > schema.json

lsp-protocol:
	bun run tools/lspgen/fetchModel.mts
	bun run tools/lspgen/generate.mts

clean:
	rm -f tally
	rm -rf bin/ dist/

# Release and publish targets
# Prerequisites:
#   - NPM_API_KEY env var (or npm login)
#   - UV_PUBLISH_TOKEN env var for PyPI
#   - ~/.gem/credentials for RubyGems

release: bin/goreleaser-$(GORELEASER_VERSION)
	bin/goreleaser release --clean --snapshot

publish-prepare: release
	cd packaging && ruby pack.rb prepare

publish-npm: publish-prepare
	cd packaging && ruby pack.rb publish_npm

publish-pypi: publish-prepare
	cd packaging && ruby pack.rb publish_pypi

publish-gem: publish-prepare
	cd packaging && ruby pack.rb publish_gem

publish: publish-prepare
	cd packaging && ruby pack.rb publish
