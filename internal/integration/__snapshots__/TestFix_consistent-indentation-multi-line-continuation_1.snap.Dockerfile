FROM ubuntu:22.04 AS builder
    ARG LAMBDA_TASK_ROOT=/var/task
    RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf \
    --mount=type=cache,target=/root/.cache/pip \
    --mount=type=secret,id=uvtoml,target=/root/.config/uv/uv.toml \
    --mount=type=bind,source=requirements.txt,target=${LAMBDA_TASK_ROOT}/requirements.txt \
    --mount=type=cache,target=/root/.cache/uv \
    pip install uv==0.9.24 && \
    uv pip install --system -r requirements.txt
FROM scratch
    COPY --from=builder /app /app
