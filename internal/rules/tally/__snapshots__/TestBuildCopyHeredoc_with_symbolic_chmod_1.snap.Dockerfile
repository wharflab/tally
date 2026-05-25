COPY --chmod=+x <<EOF /app/run.sh
#!/bin/sh
exec app
EOF