# Stage 1: /sbin/init with wrong signal (violation — replacement fix)
FROM fedora:40@sha256:3c86d25fef9d2001712bc3d9b091fc40cf04be4767e48f1aa3b785bf58d300ed AS wrong-signal
STOPSIGNAL SIGTERM
ENTRYPOINT ["/sbin/init"]

# Stage 2: systemd CMD with missing STOPSIGNAL (violation — insertion fix)
FROM centos:stream9 AS missing-signal
CMD ["/usr/lib/systemd/systemd"]

# Stage 3: /sbin/init with correct SIGRTMIN+3 (no violation)
FROM almalinux:9@sha256:606be3c7b71a40dd3b024646e9b5a0b5023d0fdf111060f249b08f04cec0ca3e AS correct
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/sbin/init"]

# Stage 4: shell-form ENTRYPOINT — PID 1 hidden (no violation)
FROM fedora:40@sha256:3c86d25fef9d2001712bc3d9b091fc40cf04be4767e48f1aa3b785bf58d300ed AS shell-form
ENTRYPOINT /sbin/init

# Stage 5: non-systemd daemon (no violation)
FROM nginx:1.27@sha256:6784fb0834aa7dbbe12e3d7471e69c290df3e6ba810dc38b34ae33d3c1c05f7d AS non-systemd
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]

# Stage 6: Windows stage — skipped (no violation)
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS windows
ENTRYPOINT ["/sbin/init"]
