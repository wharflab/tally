# Stage 1: nginx with wrong signal (violation — replacement fix, FixSuggestion)
FROM nginx:1.27@sha256:6784fb0834aa7dbbe12e3d7471e69c290df3e6ba810dc38b34ae33d3c1c05f7d AS wrong-signal
STOPSIGNAL SIGTERM
CMD ["nginx", "-g", "daemon off;"]

# Stage 2: nginx CMD with missing STOPSIGNAL (violation — insertion fix, FixSafe)
FROM nginx:1.27-alpine@sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10 AS missing-signal
CMD ["nginx", "-g", "daemon off;"]

# Stage 3: openresty with missing STOPSIGNAL (violation — insertion fix, FixSafe)
FROM openresty/openresty:alpine@sha256:2bb499890501445c30592c9f90ff2fd4bff0d751d0011bf15e80505fe3c7a6d9 AS openresty-missing
CMD ["openresty", "-g", "daemon off;"]

# Stage 4: nginx with correct SIGQUIT (no violation)
FROM nginx:1.27@sha256:6784fb0834aa7dbbe12e3d7471e69c290df3e6ba810dc38b34ae33d3c1c05f7d AS correct
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]

# Stage 5: shell-form CMD — PID 1 hidden (no violation)
FROM nginx:1.27@sha256:6784fb0834aa7dbbe12e3d7471e69c290df3e6ba810dc38b34ae33d3c1c05f7d AS shell-form
CMD nginx -g 'daemon off;'

# Stage 6: non-nginx daemon (no violation)
FROM postgres:16@sha256:ff23cdce56cac62ada6f66013e1a50864c0abbe79d132d40b6e05bd80f378a70 AS non-nginx
STOPSIGNAL SIGINT
CMD ["postgres"]

# Stage 7: Windows stage — skipped (no violation)
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS windows
CMD ["nginx", "-g", "daemon off;"]
