FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

ARG BRANCH_OFI=v1.6.0
ARG GHC_WASM_META_COMMIT=0123456789abcdef0123456789abcdef01234567

RUN echo before
ADD --link --checksum=0123456789abcdef0123456789abcdef01234567 https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567 /apex
RUN cd /apex && echo after

ADD --link https://github.com/aws/aws-ofi-nccl.git?ref=${BRANCH_OFI} /aws-ofi-nccl

ADD --link https://gitlab.haskell.org/haskell-wasm/ghc-wasm-meta.git?ref=${GHC_WASM_META_COMMIT} /ghc-wasm-meta

RUN --network=host git clone https://github.com/example/private-repo.git
