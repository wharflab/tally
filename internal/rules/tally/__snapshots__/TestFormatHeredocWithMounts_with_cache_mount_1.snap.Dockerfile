RUN --mount=type=cache,target=/var/cache/apt <<EOF
set -e
apt-get update
apt-get install -y vim
apt-get clean
EOF