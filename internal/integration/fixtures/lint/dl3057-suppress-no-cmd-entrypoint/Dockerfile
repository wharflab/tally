# Test case for DL3057 smart suppression: no CMD/ENTRYPOINT
# Image delegates run orchestration to parent — HEALTHCHECK is likely inherited
FROM nginx:latest

RUN echo "custom config" > /etc/nginx/conf.d/default.conf

EXPOSE 80
