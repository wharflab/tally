# Stage 1: wget used directly — should trigger
FROM ubuntu:22.04 AS downloader
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget
RUN wget https://github.com/dimitri/pgcopydb/archive/refs/tags/v0.17.tar.gz -O /tmp/pgcopydb.tar.gz

# Stage 2: wget config already present via RUN echo — should NOT trigger
FROM ubuntu:22.04 AS configured
RUN echo 'retry_connrefused = on' > /etc/wgetrc
RUN wget https://github.com/Kitware/CMake/releases/download/v3.17.2/cmake-3.17.2.tar.gz -O /tmp/cmake_src.tar.gz

# Stage 3: inherits from downloader — should NOT trigger (parent gets the fix)
FROM downloader AS fetcher
RUN wget https://example.com/data.json -O /tmp/data.json

# Stage 4: dotfile config already present — should NOT trigger
FROM debian:bookworm AS configured-user
RUN echo -e "retry_connrefused = on\ntimeout=15\ntries=5\nretry-on-host-error=on\n" > /root/.wgetrc
RUN wget https://github.com/unicode-org/icu/releases/download/release-67-1/icu4c-67_1-src.tgz -O /tmp/icu.tgz

# Stage 5: Windows wget.exe — should trigger
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS windows
RUN wget.exe https://example.com/bootstrap.zip -O C:\tmp\bootstrap.zip
