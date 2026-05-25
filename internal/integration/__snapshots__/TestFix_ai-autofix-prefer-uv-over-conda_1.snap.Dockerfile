FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN pip install uv && \
    uv pip install --system --index-url https://download.pytorch.org/whl/cu121 numpy torch
CMD ["python"]
