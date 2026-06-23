# syntax=docker/dockerfile:1
# Most instructions are uppercase so the lone lowercase `copy`/`workdir` are the
# minority case: tally's ConsistentInstructionCasing rule flags them and offers a
# FixSafe quick-fix that uppercases them (copy -> COPY, workdir -> WORKDIR).
FROM ubuntu:latest
RUN apt-get update && apt-get install -y curl
copy . /app
workdir /app
CMD echo hello
