FROM ubuntu:22.04

# DL3027: Should warn - using apt instead of apt-get
RUN apt update

# DL3027: Should warn - apt install
RUN apt install -y curl wget

# DL3027: Should warn - apt upgrade
RUN apt upgrade -y

# OK: Using apt-get is allowed
RUN apt-get update && \
    apt-get install -y python3

# OK: Using apt-cache is allowed
RUN apt-cache search nginx

# DL3027: Should warn - wrapped with env
RUN env DEBIAN_FRONTEND=noninteractive apt install -y vim
