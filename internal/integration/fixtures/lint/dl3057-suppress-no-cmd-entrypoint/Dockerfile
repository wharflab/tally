# Test case for DL3057 smart suppression: no CMD/ENTRYPOINT
# Image delegates run orchestration to parent — HEALTHCHECK is likely inherited
FROM nginx:latest@sha256:06aa3d7be10bc6307990c81bdca075793132e9163391abc370c015e344e23128

RUN echo "custom config" > /etc/nginx/conf.d/default.conf

EXPOSE 80
