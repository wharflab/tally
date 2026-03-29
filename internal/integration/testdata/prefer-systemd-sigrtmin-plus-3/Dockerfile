# Stage 1: /sbin/init with wrong signal (violation — replacement fix)
FROM fedora:40 AS wrong-signal
STOPSIGNAL SIGTERM
ENTRYPOINT ["/sbin/init"]

# Stage 2: systemd CMD with missing STOPSIGNAL (violation — insertion fix)
FROM centos:stream9 AS missing-signal
CMD ["/usr/lib/systemd/systemd"]

# Stage 3: /sbin/init with correct SIGRTMIN+3 (no violation)
FROM almalinux:9 AS correct
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/sbin/init"]

# Stage 4: shell-form ENTRYPOINT — PID 1 hidden (no violation)
FROM fedora:40 AS shell-form
ENTRYPOINT /sbin/init

# Stage 5: non-systemd daemon (no violation)
FROM nginx:1.27 AS non-systemd
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]

# Stage 6: Windows stage — skipped (no violation)
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS windows
ENTRYPOINT ["/sbin/init"]
