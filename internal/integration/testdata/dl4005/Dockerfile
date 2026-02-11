FROM ubuntu:22.04

# DL4005: Should warn - using ln to change /bin/sh
RUN ln -sfv /bin/bash /bin/sh

# OK: ln with unrelated target
RUN ln -sf /bin/true /sbin/initctl

# DL4005: Should warn - ln to /bin/sh in a pipeline
RUN apt-get update && ln -sf /bin/bash /bin/sh
