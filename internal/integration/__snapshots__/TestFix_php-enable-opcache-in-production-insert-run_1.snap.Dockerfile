FROM php:8.4-fpm
RUN docker-php-ext-install opcache
WORKDIR /app
COPY . .
