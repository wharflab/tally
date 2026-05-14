# syntax=docker/dockerfile:1

# Stage 1: nvidia-smi in RUN — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS gpu-check
RUN nvidia-smi && echo "GPU available"

# Stage 2: Python torch.cuda check in RUN — should fire
FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS torch-check
RUN python3 -c "import torch; print(torch.cuda.is_available())"

# Stage 3: Clean stage with no GPU queries — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS runtime
RUN apt-get update && apt-get install -y python3

# Stage 4: nvidia-smi in CMD — should NOT fire
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS service
CMD ["nvidia-smi", "--loop=10"]
