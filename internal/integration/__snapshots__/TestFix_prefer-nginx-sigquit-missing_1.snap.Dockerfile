FROM nginx:1.27
# [tally] SIGQUIT is the graceful shutdown signal for nginx
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]
