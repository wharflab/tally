FROM node:20
RUN --mount=type=cache,target=/root/.npm <<EOF
set -e
npm install
npm ci
npm install left-pad
EOF
