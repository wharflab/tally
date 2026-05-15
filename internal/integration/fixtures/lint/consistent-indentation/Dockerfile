ARG GO_VERSION=1.23
FROM golang:${GO_VERSION} AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app ./cmd/server

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS runtime
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
