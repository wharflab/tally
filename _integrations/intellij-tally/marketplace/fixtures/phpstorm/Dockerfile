# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM composer:2@sha256:b6dcd72ca98fa19d0e5b8476de179859704891fad06dd9e0b96833d4c430a5fc AS dependencies
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-interaction

FROM php:8.4-fpm@sha256:af1728147713459bda545446e3fae81cea5119543c46f28b9a077c7b3ff149c2 AS production
WORKDIR /app
COPY --from=dependencies /app/vendor ./vendor
RUN pecl install xdebug
COPY . .
CMD ["php-fpm"]
