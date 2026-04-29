FROM ubuntu:22.04

COPY <<EOF /etc/app/php.ini
zend_extension = opcache

[opcache]
  opcache.enable             = 1
  opcache.memory_consumption = 128
EOF
