FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility CUDA_HOME=/usr/local/cuda
RUN echo hello
