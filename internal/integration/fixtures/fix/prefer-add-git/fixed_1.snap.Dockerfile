FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

ARG BRANCH_OFI=v1.6.0
ARG GHC_WASM_META_COMMIT=0123456789abcdef0123456789abcdef01234567

RUN echo before
ADD --link --checksum=0123456789abcdef0123456789abcdef01234567 https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567 /apex
RUN cd /apex && echo after

ADD --link https://github.com/aws/aws-ofi-nccl.git?ref=${BRANCH_OFI} /aws-ofi-nccl

ADD --link https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git?ref=${GHC_WASM_META_COMMIT} /ghc-wasm-meta

RUN --network=host git clone https://github.com/example/private-repo.git
