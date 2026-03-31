FROM alpine:3.20
ARG BRANCH_OFI=v1.6.0
RUN echo foo
ADD --link --checksum=0123456789abcdef0123456789abcdef01234567 https://github.com/NVIDIA/apex.git?ref=0123456789abcdef0123456789abcdef01234567 /apex
RUN cd /apex && echo zoo
ADD --link https://github.com/aws/aws-ofi-nccl.git?ref=v${BRANCH_OFI} /aws-ofi-nccl
