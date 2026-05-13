FROM alpine:3.20

ARG BRANCH_OFI=v1.6.0
ARG GHC_WASM_META_COMMIT=0123456789abcdef0123456789abcdef01234567

RUN echo before
ADD --link --checksum=0123456789abcdef0123456789abcdef01234567 https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567 /apex
RUN cd /apex && echo after

ADD --link https://github.com/aws/aws-ofi-nccl.git?ref=${BRANCH_OFI} /aws-ofi-nccl

ADD --link https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git?ref=${GHC_WASM_META_COMMIT} /ghc-wasm-meta

RUN --network=host git clone https://github.com/example/private-repo.git
