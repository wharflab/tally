# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
# Most instructions are uppercase so the lone lowercase `copy`/`workdir` are the
# minority case: tally's ConsistentInstructionCasing rule flags them and offers a
# FixSafe quick-fix that uppercases them (copy -> COPY, workdir -> WORKDIR).
FROM ubuntu:latest@sha256:53958ec7b67c2c9355df922dd08dbf0360611f8c3cdb656875e81873db9ffdba
RUN apt-get update && apt-get install -y curl
copy . /app
workdir /app
CMD echo hello
