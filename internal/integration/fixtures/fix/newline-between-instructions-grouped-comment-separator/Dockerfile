FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# Add Julia to PATH
ENV PATH=/usr/local/julia/bin:$PATH \
    LD_LIBRARY_PATH=/usr/local/julia/lib/julia

# Target x86_64
ENV JULIA_CPU_TARGET="haswell"
