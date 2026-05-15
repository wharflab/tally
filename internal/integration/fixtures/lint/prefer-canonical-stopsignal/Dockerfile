# Stage 1: quoted signal (non-canonical)
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS quoted
STOPSIGNAL "SIGINT"

# Stage 2: missing SIG prefix (non-canonical)
FROM nginx:1.27 AS no-prefix
STOPSIGNAL QUIT

# Stage 3: non-canonical RT signal
FROM fedora:40 AS rt-signal
STOPSIGNAL RTMIN+3

# Stage 4: numeric signal (non-canonical)
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS numeric
STOPSIGNAL 15

# Stage 5: already canonical (no violation)
FROM postgres:16 AS canonical
STOPSIGNAL SIGINT

# Stage 6: env var reference (skipped)
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS envvar
STOPSIGNAL $MY_SIGNAL

# Stage 7: Windows stage (skipped)
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS windows
STOPSIGNAL TERM
