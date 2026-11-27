# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
FROM composer:2@sha256:5b2137f112f939a92788ceb5e55f54859d9dbf467a44037cf7760bdda949f365 AS dependencies
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-interaction

FROM php:8.4-fpm@sha256:89598ce2b089ebe8dd3772f7188a2d47300145fbcea6578de2db96c8e8f026bb AS production
WORKDIR /app
COPY --from=dependencies /app/vendor ./vendor
RUN pecl install xdebug
COPY . .
CMD ["php-fpm"]
