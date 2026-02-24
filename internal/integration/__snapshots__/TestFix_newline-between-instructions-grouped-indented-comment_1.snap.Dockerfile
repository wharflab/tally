FROM dhi.io/debian-base:trixie-dev AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked \
    apt-get update \
    && apt-get install -y build-essential curl git jq unzip xz-utils zstd

    # Haskell dependencies

ARG GHC_WASM_META_COMMIT
