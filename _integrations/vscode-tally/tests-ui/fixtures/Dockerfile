# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
# Most instructions are uppercase so the lone lowercase `copy`/`workdir` are the
# minority case: tally's ConsistentInstructionCasing rule flags them and offers a
# FixSafe quick-fix that uppercases them (copy -> COPY, workdir -> WORKDIR).
FROM ubuntu:latest@sha256:513c074113a871b51a8d16ab445c88779d6452d937a164fb5cc479f32668a41d
RUN apt-get update && apt-get install -y curl
copy . /app
workdir /app
CMD echo hello
