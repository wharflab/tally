FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
ARG BRANCH_OFI=v1.6.0
RUN echo foo  && \
    git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo zoo  
RUN git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}
