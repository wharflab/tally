FROM php:8.4-cli AS builder
RUN docker-php-ext-install xdebug

FROM php:8.4-fpm AS app
# RUN pecl install xdebug
