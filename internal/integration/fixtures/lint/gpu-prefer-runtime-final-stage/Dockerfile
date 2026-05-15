# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: builder — devel is legitimate here because nvcc is used
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS builder
RUN nvcc -o /app main.cu

# Stage 2: final stage uses devel with COPY --from but no compile signal — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17
COPY --from=builder /app /app
RUN pip install torch
CMD ["/app"]
