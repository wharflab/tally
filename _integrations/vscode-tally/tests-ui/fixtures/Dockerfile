# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
# Most instructions are uppercase so the lone lowercase `copy`/`workdir` are the
# minority case: tally's ConsistentInstructionCasing rule flags them and offers a
# FixSafe quick-fix that uppercases them (copy -> COPY, workdir -> WORKDIR).
FROM ubuntu:latest@sha256:b7f48194d4d8b763a478a621cdc81c27be222ba2206ca3ca6bc42b49685f3d9e
RUN apt-get update && apt-get install -y curl
copy . /app
workdir /app
CMD echo hello
