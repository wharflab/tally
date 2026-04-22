FROM debian:12-slim
RUN apt-get install -y php8.3-fpm php8.3-opcache
CMD ["php-fpm8.3", "-F"]
