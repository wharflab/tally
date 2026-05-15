FROM nginx:1.27@sha256:6784fb0834aa7dbbe12e3d7471e69c290df3e6ba810dc38b34ae33d3c1c05f7d
# [tally] SIGQUIT is the graceful shutdown signal for nginx / openresty
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]
