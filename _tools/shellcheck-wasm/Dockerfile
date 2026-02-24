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

RUN --mount=type=bind,source=patches,target=/patches,readonly <<EOF
set -e
./striptests
for p in /patches/*.patch; do
	[ -e "$p" ] || continue
	echo "Applying patch: $p"
	git apply "$p"
done
. ~/.ghc-wasm/env
wasm32-wasi-cabal update
wasm32-wasi-cabal build --allow-newer shellcheck
wasm-opt -o shellcheck.wasm --flatten --rereloop --converge -O3 dist-newstyle/build/wasm32-wasi/ghc-*/ShellCheck-*/x/shellcheck/build/shellcheck/shellcheck.wasm
wc -c shellcheck.wasm
EOF

FROM scratch

COPY --from=builder /shellcheck/shellcheck.wasm /shellcheck.wasm

CMD ["noop"]
