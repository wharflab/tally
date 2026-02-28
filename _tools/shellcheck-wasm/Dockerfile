FROM dhi.io/debian-base:trixie-dev AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked \
	--mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked \
	apt-get update \
	&& apt-get install -y build-essential curl git jq unzip xz-utils zstd

ARG GHC_WASM_META_COMMIT

ADD "https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git?ref=${GHC_WASM_META_COMMIT}" /ghc/

WORKDIR /ghc

RUN ./setup.sh

ARG SHELLCHECK_VERSION

ADD "https://github.com/koalaman/shellcheck.git?ref=v${SHELLCHECK_VERSION}" /shellcheck/

WORKDIR /shellcheck

COPY Reactor.hs /shellcheck/Reactor.hs

ARG AST_GREP_VERSION=0.40.5

RUN --mount=type=bind,source=rewrites,target=/rewrites,readonly <<EOF
set -e
./striptests
. ~/.ghc-wasm/env
npm install --global "@ast-grep/cli@${AST_GREP_VERSION}"
for r in /rewrites/*.yml; do
	[ -e "$r" ] || continue

	echo "Preflight rewrite bundle: $r"
	sg scan --rule "$r" --json=stream \
		shellcheck.hs src/ShellCheck/Analytics.hs src/ShellCheck/Parser.hs \
		> /tmp/sg-matches.jsonl
	awk '/^id: /{print $2}' "$r" > /tmp/sg-expected-ids.txt
	jq -r '.ruleId' /tmp/sg-matches.jsonl > /tmp/sg-found-ids.txt
	while IFS= read -r rule_id; do
		count="$(grep -cx "$rule_id" /tmp/sg-found-ids.txt || true)"
		case "$rule_id" in
			*-multi)
				[ "$count" -ge 1 ] || { echo "Rewrite rule expected >=1 match, got $count: $rule_id" >&2; exit 1; }
				;;
			*)
				[ "$count" -eq 1 ] || { echo "Rewrite rule expected exactly 1 match, got $count: $rule_id" >&2; exit 1; }
				;;
		esac
	done < /tmp/sg-expected-ids.txt

	echo "Apply rewrite bundle: $r"
	sg scan --rule "$r" --update-all shellcheck.hs src/ShellCheck/Analytics.hs src/ShellCheck/Parser.hs

	echo "Postflight rewrite bundle: $r"
	sg scan --rule "$r" --json=stream \
		shellcheck.hs src/ShellCheck/Analytics.hs src/ShellCheck/Parser.hs \
		> /tmp/sg-remaining.jsonl
	if [ -s /tmp/sg-remaining.jsonl ]; then
		echo "Rewrite bundle was not idempotent: $r" >&2
		exit 1
	fi
done

# Append reactor executable stanza to the cabal file.
cat >> ShellCheck.cabal <<'CABALEOF'

executable shellcheck-reactor
  main-is:          Reactor.hs
  build-depends:
      base >= 4.0 && < 5.0,
      ShellCheck
  default-language: Haskell2010
  ghc-options:
      -no-hs-main
      -optl-mexec-model=reactor
      "-optl-Wl,--export=hs_init,--export=sc_alloc,--export=sc_free,--export=sc_check"
CABALEOF

wasm32-wasi-cabal update
wasm32-wasi-cabal build --allow-newer shellcheck-reactor

# Find the built reactor binary and optimize it.
REACTOR_WASM=$(find dist-newstyle -name 'shellcheck-reactor.wasm' -path '*/build/shellcheck-reactor/*' | head -1)
if [ -z "$REACTOR_WASM" ]; then
    echo "ERROR: shellcheck-reactor binary not found" >&2
    find dist-newstyle -name 'shellcheck-reactor*' >&2
    exit 1
fi

wasm-opt -o shellcheck.wasm --flatten --rereloop --converge -O3 "$REACTOR_WASM"
wc -c shellcheck.wasm
EOF

FROM scratch

COPY --from=builder /shellcheck/shellcheck.wasm /shellcheck.wasm

CMD ["noop"]
