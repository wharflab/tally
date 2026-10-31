# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM composer:2@sha256:4ed71c38c6d788eb4e4e3747b5332f5b40d5b5a6fcadd5062a1a70986a118e0a AS dependencies
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-interaction

FROM php:8.4-fpm@sha256:3a6527017daca7917709afa08a37388ec108db093857f31d642cfff2e6f0449e AS production
WORKDIR /app
COPY --from=dependencies /app/vendor ./vendor
RUN pecl install xdebug
COPY . .
CMD ["php-fpm"]
