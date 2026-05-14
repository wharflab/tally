FROM ubuntu:jammy AS builder
RUN apt-get update && apt-get install -y build-essential git \
    libssl-dev zlib1g-dev pkg-config autoconf \
    automake autotools-dev autopoint libtool libgcrypt-dev libgnutls28-dev \
    libc-ares-dev \
    && rm -rf /var/lib/apt/lists/*

RUN git clone --depth=1 -b release-1.37.0 https://github.com/aria2/aria2.git
WORKDIR /aria2
RUN autoreconf -i && \
    ./configure \
    --disable-bittorrent \
    --disable-metalink \
    --disable-ftp \
    --without-libxml2 \
    --without-libexpat \
    --without-gnutls \
    --without-sqlite3 \
    --without-libssh2 \
    --disable-nls \
    --enable-static ARIA2_STATIC=yes \
    --enable-websocket \
    --enable-libaria2 \
    --with-openssl \
    --with-libcares \
    --with-ca-bundle=/etc/ssl/certs/ca-certificates.crt \
    && make -j$(nproc) \
    && make install

FROM ubuntu:jammy

COPY --from=builder /usr/local/bin/aria2c /usr/local/bin/

# Build arguments with defaults
ARG NGINX_PORT=8080
ARG ARIA2_PORT=6800
ARG MAX_CONCURRENT_DOWNLOADS=5
ARG MAX_CONNECTION_PER_SERVER=16
ARG HF_TOKEN=""
ARG ARIA2_SECRET=""
ARG CONTENT_ROOT_DIR="/aria2"
ARG ARIA_LOG_LEVEL="warn"
ARG TZ

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=${TZ:-UTC}
ENV NGINX_PORT=${NGINX_PORT}
ENV ARIA2_PORT=${ARIA2_PORT}
ENV MAX_CONCURRENT_DOWNLOADS=${MAX_CONCURRENT_DOWNLOADS}
ENV MAX_CONNECTION_PER_SERVER=${MAX_CONNECTION_PER_SERVER}
ENV HF_TOKEN=${HF_TOKEN}
ENV ARIA2_SECRET=${ARIA2_SECRET}
ENV CONTENT_ROOT_DIR=${CONTENT_ROOT_DIR}
ENV ARIA_LOG_LEVEL=${ARIA_LOG_LEVEL}

# Create a non-root user
RUN groupadd -r aria2 && useradd -r -g aria2 aria2


# Install Nginx with Lua support
RUN apt-get update && apt-get install -y --no-install-recommends \
    sudo \
    gnupg \
    gpg-agent \
    software-properties-common \
    && add-apt-repository -y ppa:ondrej/nginx-mainline \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
    nginx \
    libnginx-mod-http-lua \
    tzdata \
    ca-certificates \
    curl \
    openssl \
    unzip \
    && ln -fs /usr/share/zoneinfo/${TZ} /etc/localtime \
    && echo ${TZ} > /etc/timezone \
    && dpkg-reconfigure -f noninteractive tzdata \
    # Find the correct path to the Lua module
    && LUA_MODULE_PATH=$(find /usr/lib -name "ngx_http_lua_module.so" | head -n 1) \
    && echo "load_module \"$LUA_MODULE_PATH\";" > /etc/nginx/modules-enabled/50-mod-http-lua.conf \
    # Set up directories and rest of installation
    && mkdir -p /aria2 \
    && chown -R aria2:aria2 /aria2 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Install AriaNg
ARG ARIANG_VERSION="1.3.10"
ADD --checksum=sha256:5b76f02ff208b8c948bf8b614511687b7e6d1562323b29494528d89c032fe086 \
    https://github.com/mayswind/AriaNg/releases/download/${ARIANG_VERSION}/AriaNg-${ARIANG_VERSION}.zip /tmp/ariang.zip
RUN mkdir -p /var/www/html/ariang \
    && unzip /tmp/ariang.zip -d /var/www/html/ariang \
    && rm /tmp/ariang.zip

# Add env directive to main nginx.conf
RUN sed -i '1i env ARIA2_SECRET;' /etc/nginx/nginx.conf
RUN sed -i '1i env ARIA2_PORT;' /etc/nginx/nginx.conf

# Nginx config with Lua for environment variable access
RUN <<EOF cat > /etc/nginx/sites-available/default
server {
    listen 0.0.0.0:${NGINX_PORT};
    server_name localhost;

    # Use relative redirects instead of absolute
    absolute_redirect off;

    # Load the secret directly from env variable via Lua
    set_by_lua \$aria_port 'return os.getenv("ARIA2_PORT") or "6800"';
    set_by_lua_block \$secret_base64 {
        return ngx.encode_base64(os.getenv("ARIA2_SECRET"));
    }

    # Proxy for aria2 RPC - allows browser to access aria2 through nginx
    location /jsonrpc {
        proxy_pass http://127.0.0.1:\$aria_port/jsonrpc;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }

    # Root location handler with dynamic secret from Lua variable
    location = / {
        # Extract hostname without port
        set \$request_hostname \$http_host;
        if (\$http_host ~ "^([^:]+)") {
            set \$request_hostname \$1;
        }

         # Extract port from HTTP_HOST or use server_port as fallback
         set \$port "${NGINX_PORT}";
         if (\$http_host ~ ":([0-9]+)$") {
             set \$port \$1;
         }

        if (\$request_uri = "/") {
            # Use the Lua-generated Base64 secret
            return 302 /index.html#!/settings/rpc/set/ws/\$request_hostname/\$port/jsonrpc/\$secret_base64;
        }
    }

    location / {
        root /var/www/html/ariang;
        index index.html;
        try_files \$uri \$uri/ =404;
    }

    access_log /var/log/nginx/access.log;
    error_log /var/log/nginx/error.log;
}
EOF

# Increase file descriptor limits
RUN <<EOF cat >> /etc/security/limits.conf
aria2 soft nofile 16384
aria2 hard nofile 16384
EOF


# Aria2 config with the static secret
RUN <<EOF cat > /aria2/aria2.conf
enable-rpc=true
rpc-listen-all=true
rpc-allow-origin-all=true

# We will add Authorization header to all requests
http-auth-challenge=true

# Connection settings for large files
piece-length=5M
min-split-size=10M
split=10
max-tries=8
retry-wait=60
connect-timeout=30
timeout=120
max-file-not-found=5
max-resume-failure-tries=5
lowest-speed-limit=10K

# Connection pool optimization
reuse-uri=true
no-netrc=true

# File handling optimizations
file-allocation=falloc
disk-cache=128M
piece-length=1M
allow-overwrite=true
auto-file-renaming=false
enable-http-pipelining=true
enable-http-keep-alive=true
http-accept-gzip=true
continue=true
always-resume=false

# Performance tuning
optimize-concurrent-downloads=true
enable-mmap=true
event-poll=epoll
async-dns=true
content-disposition-default-utf8=true

# Network settings
stream-piece-selector=geom
uri-selector=adaptive
conditional-get=true
remote-time=true
server-stat-timeout=86400
disable-ipv6=true
async-dns-server=8.8.8.8,1.1.1.1
min-tls-version=TLSv1.3
user-agent=transformers/4.51.3; hf_hub/0.30.0; python/3.10.4

# Logging settings
log-level=debug
summary-interval=120

# Session management for recovery
save-session-interval=60
EOF


# Start script
RUN <<EOF cat > /start.sh
#!/bin/bash

# Create required directories
mkdir -p \${CONTENT_ROOT_DIR}/
touch \${CONTENT_ROOT_DIR}/aria2.session
chown -R aria2:aria2 \${CONTENT_ROOT_DIR}

# Start nginx as daemon first
echo "Starting nginx on port \${NGINX_PORT}..."
nginx

# Wait for nginx to become responsive
echo "Waiting for nginx to become responsive..."
if curl --output /dev/null --silent --fail --retry 10 --retry-delay 1 --retry-connrefused http://localhost:\${NGINX_PORT}/; then
    echo "AriaNg is available at http://localhost:\${NGINX_PORT}/"
else
    echo "ERROR: Nginx failed to start after 10 seconds"
    cat /var/log/nginx/error.log
    exit 1
fi

# Start aria2c in foreground (main process)
echo "Starting aria2c on port \${ARIA2_PORT}..."
echo "  Content root directory: \${CONTENT_ROOT_DIR}"
echo "  Max concurrent downloads: \${MAX_CONCURRENT_DOWNLOADS}"
echo "  Max connections per server: \${MAX_CONNECTION_PER_SERVER}"
echo "  Authorization header configured: \${HF_TOKEN:+yes}"

exec sudo -u aria2 aria2c --conf-path=/aria2/aria2.conf \
    --rpc-listen-port=\${ARIA2_PORT} \
    --rpc-secret="\${ARIA2_SECRET}" \
    --dir="\${CONTENT_ROOT_DIR}/downloads" \
    --save-session="\${CONTENT_ROOT_DIR}/aria2.session" \
    --input-file="\${CONTENT_ROOT_DIR}/aria2.session" \
    --max-concurrent-downloads=\${MAX_CONCURRENT_DOWNLOADS} \
    --max-connection-per-server=\${MAX_CONNECTION_PER_SERVER} \
    --header="Authorization: Bearer \${HF_TOKEN}" \
    --console-log-level=\${ARIA_LOG_LEVEL}
EOF
RUN chmod +x /start.sh

# Expose necessary ports
EXPOSE ${NGINX_PORT} ${ARIA2_PORT}

HEALTHCHECK --interval=1m --timeout=10s --start-period=5s --retries=3 \
    CMD curl -s \
    -H "Content-Type: application/json" \
    -H "Connection: close" \
    --data '{"jsonrpc":"2.0","method":"aria2.getGlobalStat","id":"healthcheck","params":["token:'$(printenv ARIA2_SECRET)'"]}' \
    http://localhost:$(printenv ARIA2_PORT)/jsonrpc | grep -q "result" \
    || exit 1

# Default command
CMD ["/start.sh"]
