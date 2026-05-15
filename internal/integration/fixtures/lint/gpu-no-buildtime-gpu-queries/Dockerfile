# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Stage 1: nvidia-smi in RUN — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS gpu-check
RUN nvidia-smi && echo "GPU available"

# Stage 2: Python torch.cuda check in RUN — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04@sha256:c4e81887e4aa9f13b1119337323cba89601319ecb282383b879c4ba50510fd17 AS torch-check
RUN python3 -c "import torch; print(torch.cuda.is_available())"

# Stage 3: Clean stage with no GPU queries — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS runtime
RUN apt-get update && apt-get install -y python3

# Stage 4: nvidia-smi in CMD — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04@sha256:739e0bde7bafdb2ed9057865f53085539f51cbf8bd6bf719f2e114bab321e70e AS service
CMD ["nvidia-smi", "--loop=10"]
