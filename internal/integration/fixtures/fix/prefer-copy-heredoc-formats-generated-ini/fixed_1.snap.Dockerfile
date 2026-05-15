FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

COPY <<EOF /etc/app/php.ini
zend_extension = opcache

[opcache]
  opcache.enable             = 1
  opcache.memory_consumption = 128
EOF
