# Stage 1: quoted signal (non-canonical)
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS quoted
STOPSIGNAL "SIGINT"

# Stage 2: missing SIG prefix (non-canonical)
FROM nginx:1.27@sha256:6784fb0834aa7dbbe12e3d7471e69c290df3e6ba810dc38b34ae33d3c1c05f7d AS no-prefix
STOPSIGNAL QUIT

# Stage 3: non-canonical RT signal
FROM fedora:40@sha256:3c86d25fef9d2001712bc3d9b091fc40cf04be4767e48f1aa3b785bf58d300ed AS rt-signal
STOPSIGNAL RTMIN+3

# Stage 4: numeric signal (non-canonical)
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS numeric
STOPSIGNAL 15

# Stage 5: already canonical (no violation)
FROM postgres:16@sha256:ff23cdce56cac62ada6f66013e1a50864c0abbe79d132d40b6e05bd80f378a70 AS canonical
STOPSIGNAL SIGINT

# Stage 6: env var reference (skipped)
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS envvar
STOPSIGNAL $MY_SIGNAL

# Stage 7: Windows stage (skipped)
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS windows
STOPSIGNAL TERM
