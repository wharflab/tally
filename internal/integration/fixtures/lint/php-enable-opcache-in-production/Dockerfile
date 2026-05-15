FROM composer:2@sha256:1364b5b9132ab4c42ea3be53e894572c32fe75a512cb3b1c3903fcc9bce53dcc AS deps
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-dev --no-interaction --optimize-autoloader

FROM php:8.4-fpm@sha256:121ed120cc0c125e33cd3ad9ccd7cde679e680040b21b58f8b787c28d81d9bce AS app
WORKDIR /app
COPY --from=deps /app/vendor /app/vendor
COPY . .
