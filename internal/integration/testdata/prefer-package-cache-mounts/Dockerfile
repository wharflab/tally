FROM ubuntu:24.04

RUN npm install && npm cache clean --force
RUN go build ./...
RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf \
    --mount=type=cache,target=/var/cache/apt \
    apt-get update && apt-get install -y gcc && apt-get clean
RUN apk add --no-cache curl && rm -rf /var/cache/apk/*
RUN dnf install -y git && dnf clean all
RUN yum install -y make && yum clean all
RUN zypper install -y git && zypper clean --all
RUN yarn install && yarn cache clean
RUN pip install --no-cache-dir -r requirements.txt && pip cache purge
RUN bundle install && bundle clean
WORKDIR /workspace
RUN cargo build --release
RUN dotnet restore && dotnet nuget locals all --clear
RUN composer install --no-dev && composer clear-cache
RUN uv sync --no-cache --frozen && uv cache clean
RUN bun install --no-cache && bun pm cache rm
RUN <<EOF
npm install
npm cache clean --force
EOF
