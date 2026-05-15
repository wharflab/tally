FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

ARG BRANCH_OFI=v1.6.0
ARG GHC_WASM_META_COMMIT=0123456789abcdef0123456789abcdef01234567

RUN echo before && git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo after

RUN git clone https://github.com/aws/aws-ofi-nccl.git -b ${BRANCH_OFI}

RUN git clone https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git -b ${GHC_WASM_META_COMMIT}

RUN --network=host git clone https://github.com/example/private-repo.git
