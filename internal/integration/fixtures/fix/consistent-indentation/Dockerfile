ARG GO_VERSION=1.23@sha256:60deed95d3888cc5e4d9ff8a10c54e5edc008c6ae3fba6187be6fb592e19e8c0
FROM golang:${GO_VERSION} AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app ./cmd/server

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS runtime
RUN apk --no-cache add ca-certificates tzdata
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf \
         --mount=type=cache,target=/root/.cache/pip \
--mount=type=secret,id=uvtoml,target=/root/.config/uv/uv.toml \
--mount=type=bind,source=requirements.txt,target=${LAMBDA_TASK_ROOT}/requirements.txt \
     --mount=type=cache,target=/root/.cache/uv \
  pip install uv==0.9.24 && \
      uv pip install --system -r requirements.txt
USER nobody:nobody

FROM runtime
COPY --from=builder /app /usr/local/bin/app
EXPOSE 8080
ENTRYPOINT ["app"]
