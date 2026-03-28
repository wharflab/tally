FROM composer:2 AS deps
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install

FROM composer:2 AS dev
WORKDIR /app
RUN composer install

FROM php:8.4-cli AS runtime
ENV COMPOSER_NO_DEV=1
RUN composer install

FROM php:8.4-cli AS final
WORKDIR /app
RUN composer install --no-interaction
