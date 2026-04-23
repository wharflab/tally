# Stage 1: nginx with wrong signal (violation — replacement fix, FixSuggestion)
FROM nginx:1.27 AS wrong-signal
STOPSIGNAL SIGTERM
CMD ["nginx", "-g", "daemon off;"]

# Stage 2: nginx CMD with missing STOPSIGNAL (violation — insertion fix, FixSafe)
FROM nginx:1.27-alpine AS missing-signal
CMD ["nginx", "-g", "daemon off;"]

# Stage 3: openresty with missing STOPSIGNAL (violation — insertion fix, FixSafe)
FROM openresty/openresty:alpine AS openresty-missing
CMD ["openresty", "-g", "daemon off;"]

# Stage 4: nginx with correct SIGQUIT (no violation)
FROM nginx:1.27 AS correct
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]

# Stage 5: shell-form CMD — PID 1 hidden (no violation)
FROM nginx:1.27 AS shell-form
CMD nginx -g 'daemon off;'

# Stage 6: non-nginx daemon (no violation)
FROM postgres:16 AS non-nginx
STOPSIGNAL SIGINT
CMD ["postgres"]

# Stage 7: Windows stage — skipped (no violation)
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS windows
CMD ["nginx", "-g", "daemon off;"]
