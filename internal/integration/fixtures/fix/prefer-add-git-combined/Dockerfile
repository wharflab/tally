FROM alpine:3.20
ARG BRANCH_OFI=v1.6.0
RUN echo foo  && \
    git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo zoo  
RUN git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}
