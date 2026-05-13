FROM nginx:1.27
STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]
