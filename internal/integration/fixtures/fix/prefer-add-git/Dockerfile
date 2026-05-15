FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

ARG BRANCH_OFI=v1.6.0
ARG GHC_WASM_META_COMMIT=0123456789abcdef0123456789abcdef01234567

RUN echo before && git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo after

RUN git clone https://github.com/aws/aws-ofi-nccl.git -b ${BRANCH_OFI}

RUN git clone https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git -b ${GHC_WASM_META_COMMIT}

RUN --network=host git clone https://github.com/example/private-repo.git
