FROM ubuntu:22.04
COPY --chmod=+x <<EOF /entrypoint.sh
#!/bin/sh
EOF
