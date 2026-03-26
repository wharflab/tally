# syntax=docker/dockerfile:1

# Stage 1: builder — devel is legitimate here because nvcc is used
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN nvcc -o /app main.cu

# Stage 2: final stage uses devel with COPY --from but no compile signal — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
COPY --from=builder /app /app
RUN pip install torch
CMD ["/app"]
