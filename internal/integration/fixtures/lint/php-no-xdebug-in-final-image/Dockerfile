FROM php:8.4-cli AS builder
WORKDIR /ext
RUN docker-php-ext-install xdebug

FROM php:8.4-cli AS dev
RUN pecl install xdebug && docker-php-ext-enable xdebug

FROM php:8.4-fpm AS app
WORKDIR /app
RUN docker-php-ext-install gd intl
RUN pecl install xdebug
RUN apt-get install -y php-xdebug
