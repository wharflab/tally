# Single-stage GPU Dockerfile that both installs Python ML deps via conda and
# builds from source. Needs multi-stage conversion AND conda→uv migration.
FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential git curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://repo.anaconda.com/miniconda/Miniconda3-latest-Linux-x86_64.sh -o /tmp/mc.sh && \
    bash /tmp/mc.sh -b -p /opt/conda && rm /tmp/mc.sh
ENV PATH=/opt/conda/bin:$PATH
WORKDIR /app
COPY . /app
RUN conda install -y numpy scipy && \
    conda install -y -c pytorch -c nvidia pytorch pytorch-cuda=12.1
RUN python -m compileall /app
CMD ["python", "-m", "app"]
