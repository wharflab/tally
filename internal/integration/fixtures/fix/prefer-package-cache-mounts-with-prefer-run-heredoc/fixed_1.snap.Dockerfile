FROM node:20@sha256:8f693eaa7e0a8e71560c9a82b55fd54c2ae920a2ba5d2cde28bac7d1c01c9ba5
RUN --mount=type=cache,target=/root/.npm,id=npm <<EOF
set -e
npm install
npm ci
npm install left-pad
EOF
