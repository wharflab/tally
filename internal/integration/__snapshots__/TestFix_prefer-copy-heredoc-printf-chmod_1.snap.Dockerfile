FROM ubuntu:22.04
COPY --chmod=+x <<EOF /app/run.sh
#!/bin/sh
exec app
EOF
