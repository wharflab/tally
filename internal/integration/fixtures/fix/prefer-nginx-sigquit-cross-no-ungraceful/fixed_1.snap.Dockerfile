FROM nginx:1.27
STOPSIGNAL SIGTERM
CMD ["nginx", "-g", "daemon off;"]
