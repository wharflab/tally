FROM ubuntu:24.04
RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf apt-get update && apt-get install -y gcc && apt-get clean
