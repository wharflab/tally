FROM alpine:3.20

# Add Julia to PATH
ENV PATH=/usr/local/julia/bin:$PATH \
    LD_LIBRARY_PATH=/usr/local/julia/lib/julia

# Target x86_64
ENV JULIA_CPU_TARGET="haswell"
